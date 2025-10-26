package mount

import (
	"log"
	"sync"

	"github.com/cswank/tmc2209"
	"github.com/warthog618/go-gpiocdev"
)

type (
	Declination struct {
		lock    *sync.Mutex
		motor   *tmc2209.Motor
		line    *gpiocdev.Line
		address int
		state   state
	}
)

func (d *Declination) move(decl string) (uint16, error) {
	return 0, nil
}

func (d *Declination) listen(evt gpiocdev.LineEvent) {
	d.lock.Lock()

	switch d.state {
	case 0:
		d.state++
		if err := d.motor.Move(5); err != nil {
			log.Printf("error starting motor")
		}
	case 1:
		d.state++
		if err := d.motor.Move(1); err != nil {
			log.Printf("error slowing down motor: %s", err)
		}
	default:
		d.state = 0
		if err := d.motor.Move(0); err != nil {
			log.Printf("error tracking motor: %s", err)
		}
	}

	d.lock.Unlock()
}
