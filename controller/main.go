package main

import (
	"bytes"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/alecthomas/kingpin/v2"
	_ "github.com/glebarez/go-sqlite"
	"go.bug.st/serial"

	"periph.io/x/conn/v3/driver/driverreg"
	"periph.io/x/conn/v3/physic"
	"periph.io/x/conn/v3/spi"
	"periph.io/x/conn/v3/spi/spireg"
	"periph.io/x/host/v3"
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
	db       *sql.DB

	dev  = kingpin.Arg("device", "serial device").String()
	port spi.Conn
)

func main() {
	kingpin.Parse()

	var err error
	db, err = sql.Open("sqlite", "./messier.db")
	if err != nil {
		log.Fatal(err)
	}

	if *dev != "" {
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
	}

	if _, err := host.Init(); err != nil {
		log.Fatalf("host init: %s", err)
	}

	if _, err := driverreg.Init(); err != nil {
		log.Fatalf("driverreg init: %s", err)
	}

	p, err := spireg.Open("/dev/spidev0.0")
	if err != nil {
		log.Fatalf("spi open: %s", err)
	}

	defer p.Close()

	port, err = p.Connect(physic.MegaHertz, spi.Mode3, 8)
	if err != nil {
		log.Fatalf("spi connect: %s", err)
	}

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

	ra, err := hm(obj.RA)
	if err != nil {
		fmt.Fprint(w, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	decl, err := dm(obj.Decl)
	if err != nil {
		fmt.Fprint(w, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	log.Printf("ra: %d, decl: %d", steps(ra), steps(decl))

	//TODO: make 2 jobs: GOTO and then track
	j := job{
		M1: motor{
			RPM:   400,
			Steps: steps(ra),
		},
		M2: motor{
			RPM:   400,
			Steps: steps(decl),
		},
	}

	var buf bytes.Buffer
	binary.Write(&buf, binary.LittleEndian, j)
	d := buf.Bytes()
	log.Printf("%x", d)

	// Write 0x10 to the device, and read a byte right after.
	write := append(d, 0x00)
	read := make([]byte, len(d)+1)
	if err := port.Tx(write, read); err != nil {
		log.Fatal(err)
	}
	// Use read.
	fmt.Printf("spi read: %v\n", read[1:])
}

func steps(d float64) int32 {
	return int32(d * 500 * 16)
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
