package mount

import (
	"log"
	"sync"

	"github.com/cswank/tmc2209"
	"github.com/warthog618/go-gpiocdev"
)

type (
	Declination struct {
		lock       *sync.Mutex
		motor      *tmc2209.Motor
		line       *gpiocdev.Line
		address    int
		state      state
		dec        string
		direction  float64
		microsteps int
	}
)

func (d *Declination) slewing() bool {
	return d.state == Slew || d.state == SlowSlew
}

// TODO: make sure motor always moves back across local meridian when slewing
func (d *Declination) slew(dec string) (uint16, error) {
	currentDeg, err := d.degrees(d.dec)
	if err != nil {
		return 0, err
	}

	deg, err := d.degrees(dec)
	if err != nil {
		return 0, err
	}

	deg = deg - currentDeg

	if deg < 0 {
		d.direction = -1
		deg *= -1
	} else {
		d.direction = 1
	}

	d.dec = dec

	steps := degreesToSteps(deg)
	log.Printf("dec: current deg: %f, degrees: %f, steps: %d\n", currentDeg, deg, steps)

	if steps < 100 {
		d.state = Slew
	} else {
		d.state = Ready
	}

	return steps, nil
}

func (d *Declination) degrees(s string) (float64, error) {
	degrees, minutes, err := hm(s)
	if err != nil {
		return 0, err
	}

	return degrees + (minutes / 60), nil
}

func (d *Declination) listen(evt gpiocdev.LineEvent) {
	d.lock.Lock()

	switch d.state {
	case 0:
		d.state++
		if err := d.motor.Move(5 * d.direction); err != nil {
			log.Printf("error starting motor")
		}
	case 1:
		d.state++
		if err := d.motor.Move(1 * d.direction); err != nil {
			log.Printf("error slowing down motor: %s", err)
		}
	default:
		d.state = Idle
		if err := d.motor.Move(0 * d.direction); err != nil {
			log.Printf("error stopping motor: %s", err)
		}
	}

pp	d.lock.Unlock()
}
