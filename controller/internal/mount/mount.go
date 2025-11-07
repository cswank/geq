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
	Telescope struct {
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

func New(device string, lat, lon float64, raPin, decPin int) (*Telescope, error) {
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
		port, err = serial.Open(device, mode)
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

	t := Telescope{
		port:     port,
		latitude: lat,
		ra:       RA{lock: &lock, motor: raMotor, state: Idle, ra: 0.424, longitude: lon}, //polaris's ra in radians
		dec:      Declination{dec: math.Pi / 4, lock: &lock, motor: decMotor},             //polaris's dec in radians
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

func (t *Telescope) Coordinates(lat, lon float64) {
	t.latitude = lat
	t.ra.longitude = lon
}

func (t *Telescope) GetCoordinates() (float64, float64) {
	return t.latitude, t.ra.longitude
}

func (t *Telescope) Move(axis string, hz float64) error {
	switch axis {
	case "ra":
		return t.ra.motor.Move(hz)
	case "dec":
		return t.dec.motor.Move(hz)
	}

	return nil
}

func (t *Telescope) Goto(ra, dec float64) error {
	if t.ra.slewing() || t.dec.slewing() {
		return fmt.Errorf("refusing to goto object while the mount is slewing")
	}

	ts := time.Now()
	rSteps, err := t.ra.slew(ra, ts)
	if err != nil {
		return err
	}

	dSteps, err := t.dec.slew(dec)
	if err != nil {
		return err
	}

	return t.count(rSteps, dSteps)
}

func (t *Telescope) HourAngle(ra float64, ts time.Time) string {
	lst := t.ra.localSiderealTime(ts)

	ha := lst - ((ra / (2 * math.Pi)) / 24)
	hah := math.Floor(ha)
	ham := (ha - hah) * 60

	return fmt.Sprintf("%02d:%02d", int(hah), int(ham))
}

func (t Telescope) LocalSiderealTime(ts time.Time) float64 {
	return t.ra.localSiderealTime(ts)
}

func (t Telescope) Rad(deg float64) float64 {
	return rad(deg)
}

// count sends the ra and decl steps to mcu that actually does the counting
func (t *Telescope) count(ra, dec uint16) error {
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

func (t Telescope) Close() {
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
	if len(matches) < 3 {
		return nil, fmt.Errorf("Cannot parse 'HDM' string: %s", s)
	}

	for i, m := range matches {
		matches[i] = strings.TrimSpace(m)
	}

	return matches, nil
}

// TODO: handle dec gear ratio
func radsToSteps(r float64) uint16 {
	return uint16((r / (2 * math.Pi)) * 100 * 200)
}

func rad(d float64) float64 {
	return d * (math.Pi / 180)
}
