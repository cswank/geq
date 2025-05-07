package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/alecthomas/kingpin/v2"
	"github.com/cswank/tmc2209"
	_ "github.com/glebarez/go-sqlite"

	"go.bug.st/serial"
)

type (
	object struct {
		ID            int     `json:"id"`
		NGC           *int    `json:"ngc"`
		MType         string  `json:"m_type"`
		Constellation string  `json:"constellation"`
		RA            string  `json:"ra"`
		Decl          string  `json:"decl"`
		Magnitude     float64 `json:"magnitude"`
		Name          *string `json:"name"`
	}
)

var (
	lat, lon float64
	db       *sql.DB
	motor    *tmc2209.Motor

	dev = kingpin.Arg("device", "serial device").String()
)

func main() {
	kingpin.Parse()

	var err error
	db, err = sql.Open("sqlite", "./messier.db")
	if err != nil {
		log.Fatal(err)
	}

	mode := &serial.Mode{
		BaudRate: 115200,
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	}

	port, err := serial.Open(*dev, mode)
	if err != nil {
		log.Fatalf("unable to open serial port: %s", err)
	}
	defer port.Close()

	port.SetReadTimeout(200 * time.Millisecond)

	motor = tmc2209.New(port, 0, 256)

	if err := motor.Setup(tmc2209.SpreadCycle()...); err != nil {
		log.Fatal(err)
	}

	//motor.Move(0.0005787) // I THINK this is how fast it should move with 100:1 gear reduction
	// motor.Move(10)
	// time.Sleep(10 * time.Second)
	// motor.Move(0)

	fmt.Println("slow")
	motor.Move(0.0011574) // I THINK this is how fast it should move with 100:1 gear reduction
	time.Sleep(10 * time.Second)
	motor.Move(0)

	fmt.Println("done")

	// mux := http.NewServeMux()
	// mux.HandleFunc("GET /objects", getObjects)
	// mux.HandleFunc("GET /objects/{id}", getObject)
	// mux.HandleFunc("POST /objects/{id}", gotoObject)

	// fmt.Println("Server is running on port 8080")
	// err = http.ListenAndServe(":8080", mux)
	// if err != nil {
	// 	fmt.Println("Error starting server:", err)
	// }
}

func getObjects(w http.ResponseWriter, r *http.Request) {
	q := "SELECT m, ngc, mtype, constellation, ra, decl, magnitude, name FROM messier%s"
	var clause string
	var args []any
	if s := r.URL.Query().Get("search"); s != "" {
		clause = " WHERE name LIKE ?"
		args = append(args, fmt.Sprintf("%%%s%%", s))
	}

	rows, err := db.Query(fmt.Sprintf(q, clause), args...)
	if err != nil {
		fmt.Fprint(w, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	objs := []object{}
	for rows.Next() {
		var o object
		if err := rows.Scan(&o.ID, &o.NGC, &o.MType, &o.Constellation, &o.RA, &o.Decl, &o.Magnitude, &o.Name); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, err)
			return
		}
		objs = append(objs, o)
	}

	json.NewEncoder(w).Encode(objs)
}

func getObject(w http.ResponseWriter, r *http.Request) {
	obj, err := doGetObject(r.PathValue("id"))
	if err != nil {
		fmt.Fprint(w, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(obj)
}

func gotoObject(w http.ResponseWriter, r *http.Request) {
	obj, err := doGetObject(r.PathValue("id"))
	if err != nil {
		fmt.Fprint(w, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	az, alt, lsrt, H, err := raDecToAltAz(obj.RA, obj.Decl, float64(time.Now().Unix()), lat, lon)
	if err != nil {
		fmt.Fprint(w, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	fmt.Fprintf(w, "az: %f, alt: %f, lsrt: %f, H: %f\n", az, alt, lsrt, H)
	fmt.Println(motor.Move(0.1))
}

func doGetObject(ids string) (object, error) {
	id, err := strconv.Atoi(ids)
	if err != nil {
		return object{}, err
	}

	var o object
	q := "SELECT m, ngc, mtype, constellation, ra, decl, magnitude, name FROM messier WHERE m = ?"
	return o, db.QueryRow(q, id).Scan(&o.ID, &o.NGC, &o.MType, &o.Constellation, &o.RA, &o.Decl, &o.Magnitude, &o.Name)
}

func raDecToAltAz(ras, decs string, ts, lat, lon float64) (float64, float64, float64, float64, error) {
	ra, err := hm(ras)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	dec, err := dm(decs)
	if err != nil {
		return 0, 0, 0, 0, err
	}

	//Meeus 13.5 and 13.6, modified so West longitudes are negative and 0 is North
	gmst := greenwichMeanSiderealTime(julian(ts))
	localSiderealTime := math.Mod(gmst+lon, 2*math.Pi)

	H := (localSiderealTime - ra)
	if H < 0 {
		H += 2 * math.Pi
	}
	if H > math.Pi {
		H = H - 2*math.Pi
	}

	az := (math.Atan2(math.Sin(H), math.Cos(H)*math.Sin(lat)-math.Tan(dec)*math.Cos(lat)))
	a := (math.Asin(math.Sin(lat)*math.Sin(dec) + math.Cos(lat)*math.Cos(dec)*math.Cos(H)))
	az -= math.Pi

	if az < 0 {
		az += 2 * math.Pi
	}
	return az, a, localSiderealTime, H, nil
}

func greenwichMeanSiderealTime(jd float64) float64 {
	//"Expressions for IAU 2000 precession quantities" N. Capitaine1,P.T.Wallace2, and J. Chapront
	t := (jd - 2451545.0) / 36525.0

	gmst := earthRotationAngle(jd) + (0.014506+4612.156534*t+1.3915817*t*t-0.00000044*t*t*t-0.000029956*t*t*t*t-0.0000000368*t*t*t*t*t)/60.0/60.0*math.Pi/180.0 //eq 42
	gmst = math.Mod(gmst, 2*math.Pi)
	if gmst < 0 {
		gmst += 2 * math.Pi
	}

	return gmst
}

func julian(ts float64) float64 {
	return (ts / 86400.0) + 2440587.5
}

func earthRotationAngle(jd float64) float64 {
	//IERS Technical Note No. 32
	t := jd - 2451545.0
	f := math.Mod(jd, 1.0)

	theta := 2 * math.Pi * (f + 0.7790572732640 + 0.00273781191135448*t) //eq 14
	theta = math.Mod(theta, 2*math.Pi)
	if theta < 0 {
		theta += 2 * math.Pi
	}

	return theta
}

//12:26.2
func hm(s string) (float64, error) {
	return dm(s)
}

// +12:57
func dm(s string) (float64, error) {
	var i float64
	if strings.HasPrefix(s, "+") {
		s = s[1:]
		i = 1
	} else if strings.HasPrefix(s, "-") {
		s = s[1:]
		i = -1
	}

	ss, err := splitCoord(s)
	if err != nil {
		return 0, err
	}
	f, err := parseFloats(ss[0], ss[1])
	if err != nil {
		return 0, err
	}
	dec := f[0] + f[1]/60.0
	return dec * i, nil
}

func dms(s string) (float64, error) {
	ss, err := splitCoord(s)
	if err != nil {
		return 0, err
	}
	f, err := parseFloats(ss[0], ss[1])
	if err != nil {
		return 0, err
	}
	dec := f[0] + f[1]/60.0 + f[2]/3600.0
	return dec, nil
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
