package mount

import (
	"encoding/binary"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cswank/tmc2209"
	"github.com/warthog618/go-gpiocdev"
	"go.bug.st/serial"
)

type (
	TelescopeMount struct {
		port serial.Port
		ra   RA
		decl Declination
	}

	message struct {
		Sync      uint8
		Address   uint8
		RASteps   uint16
		DeclSteps uint16
		CRC       uint8
	}

	state int
)

const (
	Idle     state = -1
	Ready    state = 0
	Slew     state = 1
	SlowSlew state = 2
	Tracking state = 3

	raMotorAddress   = 0
	declMotorAddress = 1
)

func New(device string, lon float64, raPin, declPin int) (*TelescopeMount, error) {
	mode := &serial.Mode{
		BaudRate: 115200,
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	}

	port, err := serial.Open(device, mode)
	if err != nil {
		log.Fatalf("unable to open serial port: %s", err)
	}

	raMotor := tmc2209.New(port, raMotorAddress, 200, 1)
	if err := raMotor.Setup(tmc2209.SpreadCycle()...); err != nil {
		log.Fatal(err)
	}

	declMotor := tmc2209.New(port, declMotorAddress, 200, 1)
	if err := declMotor.Setup(tmc2209.SpreadCycle()...); err != nil {
		log.Fatal(err)
	}

	var lock sync.Mutex

	t := TelescopeMount{
		port: port,
		ra:   RA{lock: &lock, motor: raMotor, state: Idle, ha: 90, longitude: lon},
		decl: Declination{lock: &lock, motor: declMotor},
	}

	t.ra.line, err = gpiocdev.RequestLine("gpiochip0", raPin, gpiocdev.WithPullUp, gpiocdev.WithBothEdges, gpiocdev.WithEventHandler(t.ra.listen))
	if err != nil {
		return nil, err
	}

	t.decl.line, err = gpiocdev.RequestLine("gpiochip0", declPin, gpiocdev.WithPullUp, gpiocdev.WithBothEdges, gpiocdev.WithEventHandler(t.decl.listen))
	if err != nil {
		return nil, err
	}

	return &t, nil
}

func (t *TelescopeMount) Goto(ra, decl string) error {
	if t.ra.state == Slew || t.ra.state == SlowSlew || t.decl.state == Slew || t.decl.state == SlowSlew {
		return fmt.Errorf("refusing to goto object while the mount is slewing")
	}

	ts := time.Now()
	rSteps, err := t.ra.move(ra, ts)
	if err != nil {
		return err
	}

	dSteps, err := t.decl.move(decl)
	if err != nil {
		return err
	}

	return t.count(rSteps, dSteps)
}

func (t *TelescopeMount) HourAngle(ra string) (float64, error) {
	return t.ra.hourAngle(ra, time.Now())
}

// count sends the ra and decl steps to mcu that actually does the counting
func (t *TelescopeMount) count(ra, decl uint16) error {
	msg := message{
		Sync:      0x5,
		Address:   0x11,
		RASteps:   ra,
		DeclSteps: decl,
	}

	buf := make([]byte, 8)
	_, err := binary.Encode(buf, binary.LittleEndian, msg)
	if err != nil {
		return err
	}

	n, err := t.port.Write(buf)
	log.Printf("uart write %d (%v)\n", n, err)
	return err

}

func (t TelescopeMount) Close() {
	t.port.Close()
	t.ra.line.Close()
	t.decl.line.Close()
}

func parseFloats(in ...string) ([]float64, error) {
	out := make([]float64, len(in))
	for i, s := range in {
		d, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, err
		}
		out[i] = d
	}

	return out, nil
}

func splitCoord(s string) ([]string, error) {
	s = strings.TrimSpace(s)
	matches := strings.Split(s, ":")
	if len(matches) < 2 {
		return nil, fmt.Errorf("Cannot parse 'HDM' string: %s", s)
	}

	for i, m := range matches {
		matches[i] = strings.TrimSpace(m)
	}

	return matches, nil
}
