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

func New(device string, lat, lon float64, raPin, declPin int) (*TelescopeMount, error) {
	mode := &serial.Mode{
		BaudRate: 115200,
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	}

	var (
		err       error
		port      serial.Port
		raMotor   *tmc2209.Motor
		declMotor *tmc2209.Motor
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

		declMotor = tmc2209.New(port, declMotorAddress, 200, 1)
		if err := declMotor.Setup(tmc2209.SpreadCycle()...); err != nil {
			log.Fatal(err)
		}

	}

	var lock sync.Mutex

	t := TelescopeMount{
		port:     port,
		latitude: lat,
		ra:       RA{lock: &lock, motor: raMotor, state: Idle, ra: "02:31.8116667", longitude: lon},
		dec:      Declination{dec: "89:15.85", lock: &lock, motor: declMotor},
	}

	if device != "" {
		t.ra.line, err = gpiocdev.RequestLine("gpiochip0", raPin, gpiocdev.WithPullUp, gpiocdev.WithBothEdges, gpiocdev.WithEventHandler(t.ra.listen))
		if err != nil {
			return nil, err
		}

		t.dec.line, err = gpiocdev.RequestLine("gpiochip0", declPin, gpiocdev.WithPullUp, gpiocdev.WithBothEdges, gpiocdev.WithEventHandler(t.dec.listen))
		if err != nil {
			return nil, err
		}
	}

	return &t, nil
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

/*
1. RA min: 0h.
2. RA max: 24h.
All hours of Right Ascension are visible within 32 degrees of the celestial pole. :-)
3. Dec min = latitude - 90
4. Dec max = 90 degrees. The celestial pole is visible.
*/
func (t TelescopeMount) Visible(id int, ra, dec string, ts time.Time) bool {
	lst := t.ra.localSiderealTime(ts)
	hours, minutes, _ := hm(ra)
	raH := hours + (minutes / 60)
	decDeg, _ := t.dec.degrees(dec)
	fmt.Printf("id: %d, lst: %f, ra: %s, ra: %f, dec: %s, dec: %f\n", id, lst, ra, raH, dec, decDeg)
	return (math.Abs(lst-raH) < 6) && decDeg < (90-t.latitude)
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
	t.dec.line.Close()
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
