package mount

import (
	"log"
	"sync"

	"github.com/cswank/tmc2209"
	"github.com/warthog618/go-gpiocdev"
)

type (
	Declination struct {
		lock      *sync.Mutex
		motor     *tmc2209.Motor
		line      *gpiocdev.Line
		address   int
		state     state
		decl      string
		direction float64
	}
)

// TODO: make sure motor always moves back accross polaris when slewing
func (d *Declination) move(decl string) (uint16, error) {
	currentDeg, err := d.degrees(d.decl)
	if err != nil {
		return 0, err
	}

	deg, err := d.degrees(decl)
	deg += currentDeg

	d.decl = decl

	if deg < 0 {
		d.direction *= -1
		deg *= -1
	} else {
		d.direction = 1
	}

	steps := uint16(((deg / 360) * 100 * 200) / 2)
	log.Printf("current deg: %f, degrees: %f, steps: %d\n", currentDeg, deg, steps)
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
		d.state = 0
		if err := d.motor.Move(0 * d.direction); err != nil {
			log.Printf("error tracking motor: %s", err)
		}
	}

	d.lock.Unlock()
}
