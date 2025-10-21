package motor

import "github.com/cswank/tmc2209"

type Motor struct {
	motor *tmc2209.Motor
}

func (m Motor) GoTo(ra, dec float64) error {
	return nil
}
