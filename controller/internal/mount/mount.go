package mount

import (
	"encoding/binary"
	"fmt"
	"log"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/cswank/tmc2209"
	"github.com/warthog618/go-gpiocdev"
	"go.bug.st/serial"
)

type (
	Mount struct {
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

func New(device string, lat, lon float64, raPin, decPin int) (*Mount, error) {
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

	t := Mount{
		port:     port,
		latitude: lat,
		ra:       RA{lock: &lock, motor: raMotor, state: Idle, ha: 0, longitude: lon},
		dec:      Declination{dec: math.Pi / 2, lock: &lock, motor: decMotor},
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

func (m *Mount) Coordinates(lat, lon float64) {
	m.latitude = lat
	m.ra.longitude = lon
}

func (m *Mount) GetCoordinates() (float64, float64) {
	return m.latitude, m.ra.longitude
}

func (m *Mount) Move(axis string, hz float64) error {
	switch axis {
	case "ra":
		return m.ra.motor.Move(hz)
	case "dec":
		return m.dec.motor.Move(hz)
	}

	return nil
}

func (m *Mount) WithRA(ra float64, ts time.Time) func() (float64, time.Time) {
	return func() (float64, time.Time) {
		lst := m.ra.localSiderealTime(ts)
		return lst - ((ra / (2 * math.Pi)) / 24), ts
	}
}

func (m *Mount) WithHA(ha float64, ts time.Time) func() (float64, time.Time) {
	return func() (float64, time.Time) {
		return rad(ha), ts
	}
}

func (m *Mount) Goto(ra func() (float64, time.Time), dec float64) error {
	ha, ts := ra()
	if m.ra.slewing() || m.dec.slewing() {
		return fmt.Errorf("refusing to goto object while the mount is slewing")
	}

	rSteps, err := m.ra.slew(ha, ts)
	if err != nil {
		return err
	}

	dSteps, err := m.dec.slew(dec)
	if err != nil {
		return err
	}

	log.Printf("ra steps: %d, dec steps: %d, dec: %f", rSteps, dSteps, dec)

	return m.count(rSteps, dSteps)
}

func (m *Mount) HourAngle(ra float64, ts time.Time) string {
	lst := m.ra.localSiderealTime(ts)

	ha := lst - ((ra / (2 * math.Pi)) / 24)
	hah := math.Floor(ha)
	ham := (ha - hah) * 60

	return fmt.Sprintf("%02d:%02d", int(hah), int(ham))
}

func (m Mount) LocalSiderealTime(ts time.Time) float64 {
	return m.ra.localSiderealTime(ts)
}

func (m Mount) Rad(deg float64) float64 {
	return rad(deg)
}

// count sends the ra and decl steps to mcu that actually does the counting
func (m *Mount) count(ra, dec uint16) error {
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

	_, err = m.port.Write(buf)
	return err

}

func (m Mount) Close() {
	m.port.Close()
	m.ra.line.Close()
	m.dec.line.Close()
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

// TODO: handle dec gear ratio
func radsToSteps(r float64) uint16 {
	return uint16(((r / (2 * math.Pi)) * 100 * 200) / 2) // divide by 2 because tmc2209 produces 2 index pulses per microstep
}

func rad(d float64) float64 {
	return d * (math.Pi / 180)
}
