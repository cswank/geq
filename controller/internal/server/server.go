package server

import (
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"text/template"

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
	}

	objects struct {
		Objects []object
	}

	Server struct {
		db    *sql.DB
		f     *vfs.FS
		mux   *http.ServeMux
		mount *mount.TelescopeMount
		idx   *template.Template
	}
)

func New(m *mount.TelescopeMount) (*Server, error) {
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

	srv := Server{
		idx:   idx,
		f:     f,
		db:    db,
		mount: m,
		mux:   http.NewServeMux(),
	}

	srv.mux.HandleFunc("GET /static/*", serveStatic)
	srv.mux.HandleFunc("GET /index", handle(srv.index))
	srv.mux.HandleFunc("GET /objects", handle(srv.getObjects))
	srv.mux.HandleFunc("GET /objects/{id}", handle(srv.getObject))
	srv.mux.HandleFunc("POST /objects/{id}", handle(srv.gotoObject))

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
	objs, err := s.doGetObjects(r)
	if err != nil {
		return err
	}

	return s.idx.ExecuteTemplate(w, "index", objects{Objects: objs})
}

func (s Server) getObjects(w http.ResponseWriter, r *http.Request) error {
	objs, err := s.doGetObjects(r)
	if err != nil {
		return err
	}

	return json.NewEncoder(w).Encode(objs)
}

func (s Server) getObject(w http.ResponseWriter, r *http.Request) error {
	obj, err := s.doGetObject(r.PathValue("id"))
	if err != nil {
		return err
	}

	obj.HA, err = s.mount.HourAngle(obj.RA)
	if err != nil {
		return err
	}

	return json.NewEncoder(w).Encode(obj)
}

func (s Server) gotoObject(w http.ResponseWriter, r *http.Request) error {
	obj, err := s.doGetObject(r.PathValue("id"))
	if err != nil {
		return err
	}

	if err := s.mount.Goto(obj.RA, obj.Decl); err != nil {
		return err
	}

	return json.NewEncoder(w).Encode(obj)
}

func (s Server) doGetObject(ids string) (object, error) {
	id, err := strconv.Atoi(ids)
	if err != nil {
		return object{}, err
	}

	var o object
	q := "SELECT m, ngc, mtype, constellation, ra, decl, magnitude, name FROM messier WHERE m = ?"
	return o, s.db.QueryRow(q, id).Scan(&o.ID, &o.NGC, &o.MType, &o.Constellation, &o.RA, &o.Decl, &o.Magnitude, &o.Name)
}

func (s Server) doGetObjects(r *http.Request) ([]object, error) {
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

	objs := []object{}
	for rows.Next() {
		var o object
		if err := rows.Scan(&o.ID, &o.NGC, &o.MType, &o.Constellation, &o.RA, &o.Decl, &o.Magnitude, &o.Name); err != nil {
			return nil, err
		}
		objs = append(objs, o)
	}

	return objs, nil
}

func serveStatic(w http.ResponseWriter, req *http.Request) {
	h := http.FileServer(http.FS(static))
	h.ServeHTTP(w, req)
}
