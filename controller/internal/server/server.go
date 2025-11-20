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
	"github.com/cswank/geq/controller/internal/repo"
	_ "modernc.org/sqlite"
	"modernc.org/sqlite/vfs"
)

var (
	//go:embed www/*
	static embed.FS
)

type (
	setup struct {
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
		Time      string  `json:"time"`
		SetTime   bool    `json:"-"`
	}

	coords struct {
		HourAngle float64 `json:"hour_angle"`
		Dec       float64 `json:"dec"`
	}

	movement struct {
		Hz    float64 `json:"hz"`
		Steps float64 `json:"steps"`
	}

	index struct {
		Objects template.JS
	}

	Server struct {
		db    *sql.DB
		f     *vfs.FS
		mux   *http.ServeMux
		mount *mount.Mount
		idx   *template.Template
		obj   *template.Template
		set   *template.Template
	}
)

func New(m *mount.Mount) (*Server, error) {
	if err := repo.Init(m); err != nil {
		return nil, err
	}

	idx, obj, pos, err := templates()
	if err != nil {
		return nil, err
	}

	srv := Server{
		idx:   idx,
		obj:   obj,
		set:   pos,
		mount: m,
		mux:   http.NewServeMux(),
	}

	srv.mux.HandleFunc("GET /", handle(srv.index))
	srv.mux.HandleFunc("POST /", handle(srv.gotoCoords))
	srv.mux.HandleFunc("GET /{id}", handle(srv.object))
	srv.mux.HandleFunc("GET /static/{pth}", handle(serveStatic))
	srv.mux.HandleFunc("GET /objects", handle(srv.getObjects))
	srv.mux.HandleFunc("GET /objects/{id}", handle(srv.getObject))
	srv.mux.HandleFunc("POST /objects/{id}", handle(srv.gotoObject))
	srv.mux.HandleFunc("GET /setup", handle(srv.setup))
	srv.mux.HandleFunc("POST /setup", handle(srv.doSetup))
	srv.mux.HandleFunc("POST /ra", handle(srv.move))
	srv.mux.HandleFunc("POST /dec", handle(srv.move))

	return &srv, nil
}

func (s Server) Start() error {
	log.Println("Server is running on port 3434")
	return http.ListenAndServe(":3434", s.mux)
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

	return s.idx.ExecuteTemplate(w, "index", nil)
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
	o, err := repo.GetObject(r.PathValue("id"))
	if err != nil {
		return err
	}

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
	obj, err := repo.GetObject(r.PathValue("id"))
	if err != nil {
		return err
	}

	return json.NewEncoder(w).Encode(obj)
}

func (s Server) gotoObject(w http.ResponseWriter, r *http.Request) error {
	obj, err := repo.GetObject(r.PathValue("id"))
	if err != nil {
		return err
	}

	if !obj.Visible {
		return fmt.Errorf("refusing to goto object that isn't visible")
	}

	if err := s.mount.Goto(s.mount.WithRA(obj.RARadians, time.Now()), obj.DecRadians); err != nil {
		return err
	}

	return json.NewEncoder(w).Encode(obj)
}

func (s Server) gotoCoords(w http.ResponseWriter, r *http.Request) error {
	var obj coords
	if err := json.NewDecoder(r.Body).Decode(&obj); err != nil {
		return err
	}

	if err := s.mount.Goto(s.mount.WithHA(obj.HourAngle, time.Now()), s.mount.Rad(obj.Dec)); err != nil {
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

func (s Server) doGetObjects(r *http.Request) (objs repo.Objects, err error) {
	var opts []repo.QueryOption
	if r.URL.Query().Get("messier") == "true" {
		opts = append(opts, repo.Messier)
	}

	if r.URL.Query().Get("named") == "true" {
		opts = append(opts, repo.Named)
	}

	if r.URL.Query().Get("visible") == "true" {
		opts = append(opts, repo.Visible)
	}

	if s := r.URL.Query().Get("name"); s != "" {
		opts = append(opts, repo.Name(s))
	}

	if types := r.URL.Query()["type"]; len(types) > 0 {
		opts = append(opts, repo.Types(types))
	}

	var pg repo.QueryOption
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

		pg = repo.Page(i, pageSize)
	}

	return repo.GetObjects(pg, opts...)
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

func templates() (*template.Template, *template.Template, *template.Template, error) {
	s, err := static.ReadFile("www/index.ghtml")
	if err != nil {
		return nil, nil, nil, err
	}

	idx, err := template.New("index").Parse(string(s))
	if err != nil {
		return nil, nil, nil, err
	}

	s, err = static.ReadFile("www/object.ghtml")
	if err != nil {
		return nil, nil, nil, err
	}

	obj, err := template.New("object").Parse(string(s))
	if err != nil {
		return nil, nil, nil, err
	}

	s, err = static.ReadFile("www/setup.ghtml")
	if err != nil {
		return nil, nil, nil, err
	}

	pos, err := template.New("setup").Parse(string(s))
	if err != nil {
		return nil, nil, nil, err
	}

	return idx, obj, pos, nil
}
