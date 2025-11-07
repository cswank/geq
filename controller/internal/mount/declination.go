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
		dec        float64
		direction  float64
		microsteps int
	}
)

func (d *Declination) slewing() bool {
	return d.state == Slew || d.state == SlowSlew
}

// TODO: make sure motor always moves back across local meridian when slewing
func (d *Declination) slew(dec float64) (uint16, error) {
	r := dec - d.dec

	if r < 0 {
		d.direction = -1
		r *= -1
	} else {
		d.direction = 1
	}

	d.dec = dec

	steps := radsToSteps(r)
	if steps < 100 {
		d.state = Slew
	} else {
		d.state = Ready
	}

	return steps, nil
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

	d.lock.Unlock()
}
