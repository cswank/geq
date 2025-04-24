package main

import (
	"bytes"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"

	_ "github.com/glebarez/go-sqlite"

	"go.bug.st/serial"
)

type (
	motor struct {
		RPM   float64
		Steps int32
	}

	job struct {
		M1   motor
		M2   motor
		Stop uint8
	}

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
)

func main() {
	db, err := sql.Open("sqlite", "./messier.db")
	if err != nil {
		log.Fatal(err)
	}

	mode := &serial.Mode{
		BaudRate: 115200,
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	}

	port, err := serial.Open("/dev/serial0", mode)
	//port, err := serial.Open("/dev/ttyUSB0", mode)
	if err != nil {
		log.Fatalf("unable to open serial port: %s", err)
	}

	defer port.Close()

	go func() {
		for {
			lg := make([]byte, 1024)
			n, err := port.Read(lg)
			if err != nil {
				log.Fatal(err)
			}
			fmt.Println(string(lg[:n]))
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /objects/", func(w http.ResponseWriter, r *http.Request) {
		q := fmt.Sprintf("%%%s%%", r.URL.Query().Get("search"))
		rows, err := db.Query("SELECT m, ngc, mtype, constellation, ra, decl, magnitude, name FROM messier WHERE name LIKE ?", q)
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
	})

	mux.HandleFunc("GET /objects/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		fmt.Fprintf(w, "handling task with id=%v\n", id)
		var buf bytes.Buffer

		j := job{
			M1: motor{
				RPM:   0,
				Steps: 0,
			},
			M2: motor{
				RPM:   100,
				Steps: 10000,
			},
		}

		binary.Write(&buf, binary.LittleEndian, j)
		d := buf.Bytes()
		log.Printf("%x", d)
		port.Write(d)
	})

	fmt.Println("Server is running on port 8080")
	err = http.ListenAndServe(":8080", mux)
	if err != nil {
		fmt.Println("Error starting server:", err)
	}
}

func raDecToAltAz(ra, dec, ts, lat, lon float64) (float64, float64, float64, float64) {
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
	return az, a, localSiderealTime, H
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

func dmsToDeg(s string, maxDeg float64) (float64, error) {
	var negDir = "s"
	if maxDeg == 180 {
		negDir = "w"
	}

	degs, mins, secs, dirs, err := splitCoord(s)
	if err != nil {
		return 0, err
	}

	deg, min, sec, err := parseFloats(degs, mins, secs)
	if err != nil {
		return 0, err
	}

	if deg > maxDeg {
		return 0, fmt.Errorf("invalid coordinate %f, degrees must be less than or equal to %f", deg, maxDeg)
	}

	if min >= 60 {
		return 0, fmt.Errorf("invalid coordinate minutes %f, must be less than 60", min)
	}

	if sec >= 60 {
		return 0, fmt.Errorf("invalid coordinate seconds %f, must be less than 60", sec)
	}

	dec := deg + min/60.0 + sec/3600.0
	if strings.ToLower(dirs) == negDir {
		dec = -dec
	}

	return dec, nil
}

func parseFloats(degs, mins, secs string) (float64, float64, float64, error) {
	deg, err := strconv.ParseFloat(degs, 64)
	if err != nil {
		return 0, 0, 0, err
	}

	min, err := strconv.ParseFloat(mins, 64)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("Cannot parse minutes %s: %s", mins, err)
	}

	sec, err := strconv.ParseFloat(secs, 64)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("Cannot parse seconds %s: %s", secs, err)
	}

	return deg, min, sec, nil
}

func splitFn(r rune) bool {
	return r == '°' || r == '\'' || r == '"'
}

// splitCoord spits degrees, minutes, seconds and direction
func splitCoord(s string) (string, string, string, string, error) {
	s = strings.TrimSpace(s)
	matches := strings.FieldsFunc(s, splitFn)
	if len(matches) < 3 {
		return "", "", "", "", fmt.Errorf("Cannot parse 'HDMS' string: %s", s)
	}

	var dir string
	if len(matches) == 4 {
		dir = strings.ToLower(strings.TrimSpace(matches[3]))
	}

	return strings.TrimSpace(matches[0]),
		strings.TrimSpace(matches[1]),
		strings.TrimSpace(matches[2]),
		dir,
		nil
}
