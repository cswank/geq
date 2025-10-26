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
	J1970 float64 = 2440587.5
)

type (
	RA struct {
		lock      *sync.Mutex
		motor     *tmc2209.Motor
		line      *gpiocdev.Line
		longitude float64
		state     state
		start     time.Time
		ra        string
		direction float64
	}
)

func (r *RA) move(ra string, t time.Time) (uint16, error) {
	currentHA, err := r.hourAngle(r.ra, t)
	if err != nil {
		return 0, err
	}

	ha, err := r.hourAngle(ra, t)
	if err != nil {
		return 0, err
	}

	deg := currentHA - ha
	if r.state == Tracking {
		d := time.Since(r.start)
		deg += (15 * (d.Minutes() / 60))
	}

	if deg < 0 {
		r.direction = -1
		deg *= -1
	} else {
		r.direction = 1
	}

	r.ra = ra
	r.start = t

	steps := uint16(((deg / 360) * 100 * 200) / 2)
	log.Printf("ra: current ha: %f, ha: %f, degrees: %f, steps: %d\n", currentHA, ha, deg, steps)

	if steps < 100 {
		r.state = Slew
	} else {
		r.state = Ready
	}

	//TODO: add more steps based on how long it will take the motor to get to the final position
	return steps, nil
}

func (r RA) hourAngle(ra string, t time.Time) (float64, error) {
	lst := r.localSiderealTime(t)
	hours, minutes, err := hm(ra)
	deg := (15 * hours) + (15 * (minutes / 60))
	return ((lst / 24) * 360) - deg, err
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
		if err := r.motor.Move(5); err != nil {
			log.Printf("error starting motor")
		}
	case Slew:
		r.state++
		if err := r.motor.Move(1); err != nil {
			log.Printf("error slowing down motor: %s", err)
		}
	case SlowSlew:
		r.state++
		//TODO: set motor microsteps to 256

		// I THINK this is how fast it should move with 100:1 gear reduction
		if err := r.motor.Move(0.0011574); err != nil {
			r.start = time.Now()
			log.Printf("error tracking motor: %s", err)
		}
	default:
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
	return float64(time)/86400000.0 + J1970
}

func hm(s string) (float64, float64, error) {
	ss, err := splitCoord(s)
	if err != nil {
		return 0, 0, err
	}
	f, err := parseFloats(ss[0], ss[1])
	if err != nil {
		return 0, 0, err
	}

	return f[0], f[1], nil
}
