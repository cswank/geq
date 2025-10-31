package server

import (
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cswank/geq/controller/internal/mount"
	_ "modernc.org/sqlite"
	"modernc.org/sqlite/vfs"
)

var (
	//go:embed files/messier.db
	dbf embed.FS

	//go:embed www/*
	static embed.FS
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
		HourAngle     string  `json:"hour_angle"`
		Visible       bool    `json:"visible"`
	}

	position struct {
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
	}

	movement struct {
		Hz float64 `json:"hz"`
	}

	objects struct {
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

func (o object) MarshalJSON() ([]byte, error) {
	var n string
	if o.Name != nil {
		n = *o.Name
	}
	return json.Marshal([]string{
		strconv.Itoa(o.ID),
		fmt.Sprintf("%t", o.Visible),
		o.HourAngle,
		n,
	})
}

func New(m *mount.Telescope) (*Server, error) {
	fn, f, err := vfs.New(dbf)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", "file:files/messier.db?vfs="+fn)
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
	srv.mux.HandleFunc("POST /setup", handle(srv.coordinates))
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
	fmt.Println(lat, lon)
	if lat == 0 && lon == 0 {
		return s.set.ExecuteTemplate(w, "position", nil)
	}

	objs, err := s.doGetObjects(r)
	if err != nil {
		return err
	}

	j, err := json.Marshal(objs)
	if err != nil {
		return err
	}

	return s.idx.ExecuteTemplate(w, "index", objects{Objects: template.JS(j)})
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

func (s *Server) coordinates(w http.ResponseWriter, r *http.Request) error {
	var p position
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		return err
	}

	s.mount.Coordinates(p.Latitude, p.Longitude)
	return nil
}

func (s *Server) move(w http.ResponseWriter, r *http.Request) error {
	var m movement
	if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
		return err
	}

	return s.mount.Move(strings.ReplaceAll(r.URL.Path, "/", ""), m.Hz)
}

func (s Server) doGetObject(r *http.Request) (object, error) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		return object{}, err
	}

	var o object
	q := "SELECT m, ngc, mtype, constellation, ra, decl, magnitude, name FROM messier WHERE m = ?"
	return o, s.db.QueryRow(q, id).Scan(&o.ID, &o.NGC, &o.MType, &o.Constellation, &o.RA, &o.Decl, &o.Magnitude, &o.Name)
}

func (s Server) doGetObjects(r *http.Request) ([]object, error) {
	ts := time.Now()
	q := "SELECT m, ngc, mtype, constellation, ra, decl, magnitude, name FROM messier%s"
	var clause string
	var args []any
	if s := r.URL.Query().Get("search"); s != "" {
		clause = " WHERE name LIKE ?"
		args = append(args, fmt.Sprintf("%%%s%%", s))
	}

	rows, err := s.db.Query(fmt.Sprintf(q, clause), args...)
	if err != nil {
		return nil, err
	}

	vis := r.URL.Query().Get("visible") == "true"

	objs := []object{}
	for rows.Next() {
		var o object
		if err := rows.Scan(&o.ID, &o.NGC, &o.MType, &o.Constellation, &o.RA, &o.Decl, &o.Magnitude, &o.Name); err != nil {
			return nil, err
		}

		o.Visible = s.mount.Visible(o.ID, o.RA, o.Decl, ts)
		o.HourAngle = s.mount.HourAngle(o.RA, ts)
		if !vis || o.Visible {
			objs = append(objs, o)
		}
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
