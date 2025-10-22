package main

import (
	"database/sql"
	"encoding/binary"
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
		HA            float64 `json:"ha"`
	}
)

var (
	lat, lon float64
	db       *sql.DB
	motor    *tmc2209.Motor
	port     serial.Port

	dev = kingpin.Arg("device", "serial device").String()
)

func main() {
	kingpin.Parse()

	lon = -104.99

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

	port, err = serial.Open(*dev, mode)
	if err != nil {
		log.Fatalf("unable to open serial port: %s", err)
	}
	defer port.Close()

	// motor = tmc2209.New(port, 0, 200, 256)

	// if err := motor.Setup(tmc2209.SpreadCycle()...); err != nil {
	// 	log.Fatal(err)
	// }

	// fmt.Println("slow")
	// motor.Move(0.0011574) // I THINK this is how fast it should move with 100:1 gear reduction
	// time.Sleep(10 * time.Second)
	// motor.Move(0)

	// fmt.Println("done")

	mux := http.NewServeMux()
	mux.HandleFunc("GET /objects", getObjects)
	mux.HandleFunc("GET /objects/{id}", getObject)
	mux.HandleFunc("POST /objects/{id}", gotoObject)

	fmt.Println("Server is running on port 8080")
	err = http.ListenAndServe(":8080", mux)
	if err != nil {
		fmt.Println("Error starting server:", err)
	}
}

type message struct {
	Sync         uint8
	Address      uint8
	Microdegrees uint32
	CRC          uint8
}

func write(microdegrees uint32) error {
	msg := message{
		Sync:         0x5,
		Address:      111,
		Microdegrees: microdegrees,
	}

	buf := make([]byte, 8)
	_, err := binary.Encode(buf, binary.BigEndian, msg)
	if err != nil {
		return err
	}

	_, err = port.Write(buf)
	return err
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

	obj.HA, err = hourAngle(obj.RA, time.Now())
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

	obj.HA, err = hourAngle(obj.RA, time.Now())
	if err != nil {
		fmt.Fprint(w, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	deg := math.Abs(90 - obj.HA)
	microdegrees := uint32(deg * 1000)

	if write(microdegrees) != nil {
		fmt.Fprint(w, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// if err := motor.Move(10); err != nil {
	// 	fmt.Fprint(w, err)
	// 	w.WriteHeader(http.StatusInternalServerError)
	// 	return
	// }

	json.NewEncoder(w).Encode(obj)
}

func hourAngle(ra string, t time.Time) (float64, error) {
	lst := localSiderealTime(t)
	hours, minutes, err := hm(ra)
	deg := (15 * hours) + (15 * (minutes / 60))
	return ((lst / 24) * 360) - deg, err
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

const J1970 float64 = 2440587.5

func localSiderealTime(datetime time.Time) float64 {
	gst := greenwichSiderealTime(datetime)

	d := (gst + lon/15.0) / 24.0
	d -= math.Floor(d)
	if d < 0 {
		d += 1
	}

	return 24.0 * d
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
