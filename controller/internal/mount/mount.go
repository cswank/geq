package mount

import (
	"encoding/binary"
	"fmt"
	"log"
	"math"
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
		port     serial.Port
		latitude float64
		ra       RA
		dec      Declination
	}

	message struct {
		Sync             uint8
		Address          uint8
		RASteps          uint16
		DeclinationSteps uint16
		CRC              uint8
	}

	state int
)

const (
	Idle     state = -1
	Ready    state = 0
	Slew     state = 1
	SlowSlew state = 2
	Tracking state = 3

	raMotorAddress  = 0
	decMotorAddress = 1
)

func New(device string, lat, lon float64, raPin, decPin int) (*TelescopeMount, error) {
	mode := &serial.Mode{
		BaudRate: 115200,
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	}

	var (
		err      error
		port     serial.Port
		raMotor  *tmc2209.Motor
		decMotor *tmc2209.Motor
	)
	if device != "" {
		port, err := serial.Open(device, mode)
		if err != nil {
			log.Fatalf("unable to open serial port: %s", err)
		}

		raMotor = tmc2209.New(port, raMotorAddress, 200, 1)
		if err := raMotor.Setup(tmc2209.SpreadCycle()...); err != nil {
			log.Fatal(err)
		}

		decMotor = tmc2209.New(port, decMotorAddress, 200, 1)
		if err := decMotor.Setup(tmc2209.SpreadCycle()...); err != nil {
			log.Fatal(err)
		}

	}

	var lock sync.Mutex

	t := TelescopeMount{
		port:     port,
		latitude: lat,
		ra:       RA{lock: &lock, motor: raMotor, state: Idle, ra: "02:31.8116667", longitude: lon},
		dec:      Declination{dec: "89:15.85", lock: &lock, motor: decMotor},
	}

	if device != "" {
		t.ra.line, err = gpiocdev.RequestLine("gpiochip0", raPin, gpiocdev.WithPullUp, gpiocdev.WithBothEdges, gpiocdev.WithEventHandler(t.ra.listen))
		if err != nil {
			return nil, err
		}

		t.dec.line, err = gpiocdev.RequestLine("gpiochip0", decPin, gpiocdev.WithPullUp, gpiocdev.WithBothEdges, gpiocdev.WithEventHandler(t.dec.listen))
		if err != nil {
			return nil, err
		}
	}

	return &t, nil
}

func (t *TelescopeMount) Position(lat, lon float64) {
	t.latitude = lat
	t.ra.longitude = lon
}

func (t *TelescopeMount) Goto(ra, dec string) error {
	if t.ra.state == Slew || t.ra.state == SlowSlew || t.dec.state == Slew || t.dec.state == SlowSlew {
		return fmt.Errorf("refusing to goto object while the mount is slewing")
	}

	ts := time.Now()
	rSteps, err := t.ra.move(ra, ts)
	if err != nil {
		return err
	}

	dSteps, err := t.dec.move(dec)
	if err != nil {
		return err
	}

	return t.count(rSteps, dSteps)
}

func (t *TelescopeMount) HourAngle(ra string) (float64, error) {
	return t.ra.hourAngle(ra, time.Now())
}

func (t TelescopeMount) Visible(id int, ra, dec string, ts time.Time) bool {
	ha, _ := t.ra.hourAngle(ra, ts)
	deg, _ := t.dec.degrees(dec)
	return t.circumpolar(deg) || t.aboveHorizon(ha, deg)
}

// count sends the ra and decl steps to mcu that actually does the counting
func (t *TelescopeMount) count(ra, dec uint16) error {
	msg := message{
		Sync:             0x5,
		Address:          0x11,
		RASteps:          ra,
		DeclinationSteps: dec,
	}

	buf := make([]byte, 8)
	_, err := binary.Encode(buf, binary.LittleEndian, msg)
	if err != nil {
		return err
	}

	_, err = t.port.Write(buf)
	return err

}

func (t TelescopeMount) Close() {
	t.port.Close()
	t.ra.line.Close()
	t.dec.line.Close()
}

func (t TelescopeMount) aboveHorizon(ha, dec float64) bool {
	return math.Cos(rad(ha)) > -1*math.Tan(rad(t.latitude))*math.Tan(rad(dec))
}

func (t TelescopeMount) circumpolar(dec float64) bool {
	return dec > 90-t.latitude
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

func degreesToSteps(deg float64) uint16 {
	return uint16(((deg / 360) * 100 * 200) / 2)
}

func rad(d float64) float64 {
	return d * (math.Pi / 180)
}
