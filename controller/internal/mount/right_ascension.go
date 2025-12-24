package mount

import (
	"log"
	"math"
	"sync"
	"time"

	"github.com/cswank/tmc2209"
	"github.com/warthog618/go-gpiocdev"
)

const (
	j1970         float64 = 2440587.5
	trackingSpeed float64 = -100.0 / (24.0 * 60.0)
)

type (
	RA struct {
		lock       *sync.Mutex
		motor      *tmc2209.Motor
		line       *gpiocdev.Line
		longitude  float64
		state      state
		direction  float64
		microsteps int
		gearRatio  float64

		// start is the time at which tracking began
		start time.Time
		// ha is the hour angle of the object being tracked
		ha float64
	}
)

func (r *RA) slewing() bool {
	return r.state == Slew || r.state == SlowSlew
}

func (r *RA) slew(ha float64, t time.Time) (uint16, error) {
	rads := ha - r.ha
	if r.state == Tracking {
		rads += degreesToRadians(15 * time.Since(r.start).Minutes() / 60)
	}

	if rads < 0 {
		r.direction = -1
		rads *= -1
	} else {
		r.direction = 1
	}

	steps := r.radsToSteps(rads)
	log.Printf("ra: current ha: %f, ha: %f, radians: %f, steps: %d, diration: %f\n", r.ha, ha, rads, steps, r.direction)

	r.ha = ha
	r.start = t

	if steps < 100 {
		r.state = Slew
	} else {
		r.state = Ready
	}

	//TODO: add more steps based on how long it will take the motor to get to the final position
	return steps, nil
}

func (r RA) radsToSteps(rads float64) uint16 {
	return radiansToSteps(rads, r.gearRatio)
}

func (r RA) localSiderealTime(datetime time.Time) float64 {
	gst := greenwichSiderealTime(datetime)

	d := (gst + r.longitude/15.0) / 24.0
	d -= math.Floor(d)
	if d < 0 {
		d += 1
	}

	return 24.0 * d
}

func (r *RA) listen(evt gpiocdev.LineEvent) {
	r.lock.Lock()

	switch r.state {
	case Ready:
		r.state++
		if err := r.motor.Microsteps(1); err != nil {
			log.Printf("error setting microsteps: %s", err)
		}
		if err := r.motor.Move(5 * r.direction); err != nil {
			log.Printf("error starting motor")
		}
	case Slew:
		r.state++
		if err := r.motor.Move(1 * r.direction); err != nil {
			log.Printf("error slowing down motor: %s", err)
		}
	case SlowSlew:
		r.state++
		if err := r.motor.Microsteps(256); err != nil {
			log.Printf("error setting microsteps: %s", err)
		}
		if err := r.motor.Move(trackingSpeed); err != nil {
			log.Printf("error tracking motor: %s", err)
		}
		r.start = time.Now()
	default:
		r.state = Idle
		if err := r.motor.Move(0); err != nil {
			log.Printf("error stopping motor: %s", err)
		}
	}

	r.lock.Unlock()
}

func greenwichSiderealTime(datetime time.Time) float64 {
	jd := julianDate(datetime)
	jd0 := julianDate(time.Date(datetime.Year(), 1, 0, 0, 0, 0, 0, time.UTC))
	t := (jd0 - 2415020.0) / 36525
	besselianStarYear := 24.0 - (6.6460656 + 2400.051262*t + 0.00002581*math.Pow(t, 2)) + float64(24*(datetime.Year()-1900))
	t0 := 0.0657098*math.Floor(jd-jd0) - besselianStarYear
	ut := (float64(datetime.UnixMilli()) - float64(time.Date(datetime.Year(), datetime.Month(), datetime.Day(), 0, 0, 0, 0, time.UTC).UnixMilli())) / 3600000
	a := ut * 1.002737909
	gst := math.Mod(t0+a, 24)

	if gst < 0 {
		gst += 24
	}

	return math.Mod(gst, 24)
}

func julianDate(datetime time.Time) float64 {
	var time int64 = datetime.UTC().UnixNano() / 1e6
	return float64(time)/86400000.0 + j1970
}
