package server

import (
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/cswank/geq/controller/internal/mount"
	"github.com/parsyl/sqrl"
	_ "modernc.org/sqlite"
	"modernc.org/sqlite/vfs"
)

var (
	//go:embed files/objects.db
	dbf embed.FS

	//go:embed www/*
	static embed.FS

	columns = []string{"id", "type", "constelation", "ra", "dec", "magnitude", "name", "m"}
)

type (
	object struct {
		ID            string   `json:"id"`
		M             *int     `json:"m"`
		NGC           *int     `json:"ngc"`
		Type          string   `json:"type"`
		Constellation *string  `json:"constellation"`
		RA            string   `json:"ra"`
		Decl          string   `json:"decl"`
		Magnitude     *float64 `json:"magnitude"`
		Name          *string  `json:"name"`
		HA            float64  `json:"ha"`
		HourAngle     string   `json:"hour_angle"`
		Visible       bool     `json:"visible"`
	}

	objects struct {
		Objects []object `json:"objects"`
		Total   int      `json:"total"`
	}

	setup struct {
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
		Time      string  `json:"time"`
		SetTime   bool    `json:"-"`
	}

	movement struct {
		Hz float64 `json:"hz"`
	}

	index struct {
		Objects template.JS
	}

	Server struct {
		db    *sql.DB
		f     *vfs.FS
		mux   *http.ServeMux
		mount *mount.Telescope
		idx   *template.Template
		obj   *template.Template
		set   *template.Template
	}
)

func New(m *mount.Telescope) (*Server, error) {
	fn, f, err := vfs.New(dbf)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", "file:files/objects.db?vfs="+fn)
	if err != nil {
		return nil, err
	}

	s, err := static.ReadFile("www/index.ghtml")
	if err != nil {
		return nil, err
	}

	idx, err := template.New("index").Parse(string(s))
	if err != nil {
		return nil, err
	}

	s, err = static.ReadFile("www/object.ghtml")
	if err != nil {
		return nil, err
	}

	obj, err := template.New("object").Parse(string(s))
	if err != nil {
		return nil, err
	}

	s, err = static.ReadFile("www/setup.ghtml")
	if err != nil {
		return nil, err
	}

	pos, err := template.New("setup").Parse(string(s))
	if err != nil {
		return nil, err
	}

	srv := Server{
		idx:   idx,
		obj:   obj,
		set:   pos,
		f:     f,
		db:    db,
		mount: m,
		mux:   http.NewServeMux(),
	}

	srv.mux.HandleFunc("GET /", handle(srv.index))
	srv.mux.HandleFunc("GET /{id}", handle(srv.object))
	srv.mux.HandleFunc("GET /static/{pth}", handle(serveStatic))
	srv.mux.HandleFunc("GET /objects", handle(srv.getObjects))
	srv.mux.HandleFunc("GET /objects/{id}", handle(srv.getObject))
	srv.mux.HandleFunc("POST /objects/{id}", handle(srv.gotoObject))
	srv.mux.HandleFunc("GET /setup", handle(srv.setup))
	srv.mux.HandleFunc("POST /coordinates", handle(srv.doSetup))
	srv.mux.HandleFunc("POST /ra", handle(srv.move))
	srv.mux.HandleFunc("POST /dec", handle(srv.move))

	return &srv, nil
}

func (s Server) Start() error {
	log.Println("Server is running on port 8080")
	return http.ListenAndServe(":8080", s.mux)
}

type handler func(w http.ResponseWriter, r *http.Request) error

func handle(f handler) func(w http.ResponseWriter, r *http.Request) {
	var err error
	return func(w http.ResponseWriter, r *http.Request) {
		err = f(w, r)
		if err != nil {
			log.Printf("error: %f", err)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}
}

func (s Server) index(w http.ResponseWriter, r *http.Request) error {
	lat, lon := s.mount.GetCoordinates()
	if lat == 0 && lon == 0 {
		return s.setup(w, r)
	}

	objs, err := s.doGetObjects(r)
	if err != nil {
		return err
	}

	j, err := json.Marshal(objs)
	if err != nil {
		return err
	}

	return s.idx.ExecuteTemplate(w, "index", index{Objects: template.JS(j)})
}

func (s Server) setup(w http.ResponseWriter, r *http.Request) error {
	ts := time.Now()
	lat, lon := s.mount.GetCoordinates()
	return s.set.ExecuteTemplate(w, "setup", setup{
		SetTime:   ts.Year() < 2025, // no internet, need to manually set time
		Time:      ts.Format("2006-01-02T04:05"),
		Latitude:  lat,
		Longitude: lon,
	})
}

func (s *Server) doSetup(w http.ResponseWriter, r *http.Request) error {
	var p setup
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		return err
	}

	s.mount.Coordinates(p.Latitude, p.Longitude)

	if time.Now().Year() < 2025 {
		ts, err := time.Parse("2006-01-02T04:05", p.Time)
		if err != nil {
			return err
		}
		//date -s '2014-12-25 12:34:56'
		args := []string{"--set", ts.Format("2006-01-01 04:05:06")}
		if err := exec.Command("date", args...).Run(); err != nil {
			return err
		}
	}
	return nil
}

func (s Server) object(w http.ResponseWriter, r *http.Request) error {
	o, err := s.doGetObject(r)
	if err != nil {
		return err
	}

	o.HourAngle = s.mount.HourAngle(o.RA, time.Now())
	return s.obj.ExecuteTemplate(w, "object", o)
}

func (s Server) getObjects(w http.ResponseWriter, r *http.Request) error {
	objs, err := s.doGetObjects(r)
	if err != nil {
		return err
	}

	return json.NewEncoder(w).Encode(objs)
}

func (s Server) getObject(w http.ResponseWriter, r *http.Request) error {
	obj, err := s.doGetObject(r)
	if err != nil {
		return err
	}

	obj.HourAngle = s.mount.HourAngle(obj.RA, time.Now())

	return json.NewEncoder(w).Encode(obj)
}

func (s Server) gotoObject(w http.ResponseWriter, r *http.Request) error {
	obj, err := s.doGetObject(r)
	if err != nil {
		return err
	}

	if err := s.mount.Goto(obj.RA, obj.Decl); err != nil {
		return err
	}

	return json.NewEncoder(w).Encode(obj)
}

func (s *Server) move(w http.ResponseWriter, r *http.Request) error {
	var m movement
	if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
		return err
	}

	return s.mount.Move(strings.ReplaceAll(r.URL.Path, "/", ""), m.Hz)
}

func (s Server) doGetObject(r *http.Request) (object, error) {
	id := r.PathValue("id")

	var o object
	q := `SELECT id, type, constelation, ra, dec, magnitude, name FROM objects WHERE id = ?`
	if err := s.db.QueryRow(q, id).Scan(&o.ID, &o.Type, &o.Constellation, &o.RA, &o.Decl, &o.Magnitude, &o.Name); err != nil {
		return o, err
	}

	o.Visible = s.mount.Visible(o.ID, o.RA, o.Decl, time.Now())

	return o, nil
}

func (s Server) doGetObjects(r *http.Request) (objs objects, err error) {
	cte := sqrl.Select(columns...).
		From("objects")
	if r.URL.Query().Get("messier") == "true" {
		cte.Where("m IS NOT NULL").
			OrderBy("m")
	}

	if r.URL.Query().Get("named") == "true" {
		cte.Where("name IS NOT NULL")
	}

	if r.URL.Query().Get("visible") == "true" {
		t := time.Now()
		lst := s.mount.LocalSiderealTime(t)
		lst = (lst / 24) * 360
		lat, _ := s.mount.GetCoordinates()
		cte.Where("((dec_degrees > ?) OR (cos(? - ra_degrees) > (-1*tan(?)*tan(dec_degrees))))", s.mount.Rad(90-lat), s.mount.Rad(lst), s.mount.Rad(lat))
	}

	if s := r.URL.Query().Get("name"); s != "" {
		cte.Where("name LIKE ?", fmt.Sprintf("%%%s%%", s))
	}

	q, args, _ := cte.ToSql()
	count := fmt.Sprintf("WITH objs AS (%s) SELECT count(*) FROM objs", q)

	if err := s.db.QueryRow(count, args...).Scan(&objs.Total); err != nil {
		return objs, err
	}

	ts := time.Now()
	sel := sqrl.Select(columns...).
		From("objs").
		Prefix(fmt.Sprintf("WITH objs AS (%s)", q), args...)

	if s := r.URL.Query().Get("page"); s != "" {
		pageSize := 20
		if s := r.URL.Query().Get("pagesize"); s != "" {
			i, err := strconv.Atoi(s)
			if err != nil {
				return objs, err
			}
			pageSize = i
		}

		i, err := strconv.Atoi(s)
		if err != nil {
			return objs, err
		}

		sel.Limit(uint64(pageSize)).Offset(uint64(i * pageSize))
	}

	q, args, _ = sel.ToSql()
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return objs, err
	}

	for rows.Next() {
		var o object
		if err := rows.Scan(&o.ID, &o.Type, &o.Constellation, &o.RA, &o.Decl, &o.Magnitude, &o.Name, &o.M); err != nil {
			return objs, err
		}

		o.Visible = s.mount.Visible(o.ID, o.RA, o.Decl, ts)
		o.HourAngle = s.mount.HourAngle(o.RA, ts)
		objs.Objects = append(objs.Objects, o)
	}

	return objs, nil
}

func serveStatic(w http.ResponseWriter, req *http.Request) error {
	pth := req.URL.Path
	pth = strings.Replace(pth, "/static/", "www/", 1)
	if strings.HasSuffix(pth, ".css") {
		w.Header().Add("content-type", "text/css")
	} else {
		w.Header().Add("content-type", "text/javascript")
	}
	p, err := static.ReadFile(pth)
	if err != nil {
		return err
	}
	_, err = w.Write(p)
	return err
}
