package server

import (
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/cswank/geq/controller/internal/mount"
	_ "modernc.org/sqlite"
	"modernc.org/sqlite/vfs"
)

var (
	//go:embed files/messier.db
	dbf embed.FS
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

	Server struct {
		db    *sql.DB
		f     *vfs.FS
		mux   *http.ServeMux
		mount *mount.TelescopeMount
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

	s := Server{
		f:     f,
		db:    db,
		mount: m,
		mux:   http.NewServeMux(),
	}

	s.mux.HandleFunc("GET /objects", s.getObjects)
	s.mux.HandleFunc("GET /objects/{id}", s.getObject)
	s.mux.HandleFunc("POST /objects/{id}", s.gotoObject)

	return &s, nil
}

func (s Server) Start() error {
	log.Println("Server is running on port 8080")
	return http.ListenAndServe(":8080", s.mux)
}

func (s Server) getObjects(w http.ResponseWriter, r *http.Request) {
	q := "SELECT m, ngc, mtype, constellation, ra, decl, magnitude, name FROM messier%s"
	var clause string
	var args []any
	if s := r.URL.Query().Get("search"); s != "" {
		clause = " WHERE name LIKE ?"
		args = append(args, fmt.Sprintf("%%%s%%", s))
	}

	rows, err := s.db.Query(fmt.Sprintf(q, clause), args...)
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

func (s Server) getObject(w http.ResponseWriter, r *http.Request) {
	obj, err := s.doGetObject(r.PathValue("id"))
	if err != nil {
		fmt.Fprint(w, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	obj.HA, err = s.mount.HourAngle(obj.RA)
	if err != nil {
		fmt.Fprint(w, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(obj)
}

func (s Server) gotoObject(w http.ResponseWriter, r *http.Request) {
	obj, err := s.doGetObject(r.PathValue("id"))
	if err != nil {
		fmt.Fprint(w, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if err := s.mount.Goto(obj.RA, obj.Decl); err != nil {
		fmt.Fprint(w, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(obj)
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
