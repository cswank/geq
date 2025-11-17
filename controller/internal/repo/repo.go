package repo

import (
	"database/sql"
	"database/sql/driver"
	"embed"
	"fmt"
	"time"

	"github.com/cswank/geq/controller/internal/mount"
	"github.com/parsyl/sqrl"
	"modernc.org/sqlite"
	_ "modernc.org/sqlite"
	"modernc.org/sqlite/vfs"
)

var (
	//go:embed files/objects.db
	dbf embed.FS

	columns = []string{
		"id",
		"type",
		"constelation",
		"ra",
		"dec",
		"ra_radians",
		"dec_radians",
		"magnitude",
		"name",
		"m",
		"hour_angle(ra_radians)",
	}

	fs *vfs.FS
	db *sql.DB

	mnt *mount.Mount
)

type (
	Object struct {
		ID            string   `json:"id"`
		M             *int     `json:"m"`
		NGC           *int     `json:"ngc"`
		Type          string   `json:"type"`
		Constellation *string  `json:"constellation"`
		RA            string   `json:"ra"`
		Dec           string   `json:"dec"`
		RARadians     float64  `json:"ra_radians"`
		DecRadians    float64  `json:"dec_radians"`
		Magnitude     *float64 `json:"magnitude"`
		Name          *string  `json:"name"`
		HA            float64  `json:"ha"`
		HourAngle     string   `json:"hour_angle"`
		Visible       bool     `json:"visible"`
	}

	Objects struct {
		Objects []Object `json:"objects"`
		Total   int      `json:"total"`
	}

	QueryOption func(*sqrl.SelectBuilder)
)

func Init(m *mount.Mount) (err error) {
	mnt = m

	var fn string
	fn, fs, err = vfs.New(dbf)
	if err != nil {
		return err
	}

	if err := sqlite.RegisterScalarFunction("hour_angle", 1, hourAngle); err != nil {
		return fmt.Errorf("unable to register hour_angle func: %s", err)
	}

	db, err = sql.Open("sqlite", "file:files/objects.db?vfs="+fn)
	if err != nil {
		return err
	}

	return nil
}

func hourAngle(ctx *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
	ra := args[0].(float64)
	return mnt.HourAngle(ra, time.Now()), nil
}

func GetObject(id string) (o Object, err error) {
	sel := sqrl.Select(columns...).
		From("objects")

	visible(sel)

	sel.Where("id = ?", id)
	q, args, _ := sel.ToSql()

	return o, db.QueryRow(q, args...).Scan(&o.ID, &o.Type, &o.Constellation, &o.RA, &o.Dec, &o.RARadians, &o.DecRadians, &o.Magnitude, &o.Name, &o.M, &o.HourAngle, &o.Visible)
}

func GetObjects(page QueryOption, opts ...QueryOption) (objs Objects, err error) {
	cte := sqrl.Select(columns...).
		From("objects").
		OrderBy("id")
	visible(cte)

	for _, o := range opts {
		o(cte)
	}

	q, args, _ := cte.ToSql()
	count := fmt.Sprintf("WITH objs AS (%s) SELECT count(*) FROM objs", q)

	if err := db.QueryRow(count, args...).Scan(&objs.Total); err != nil {
		return objs, err
	}

	sel := sqrl.Select(append(columns, "visible")...).
		From("objs").
		Prefix(fmt.Sprintf("WITH objs AS (%s)", q), args...)

	if page != nil {
		page(sel)
	}

	q, args, _ = sel.ToSql()
	rows, err := db.Query(q, args...)
	if err != nil {
		return objs, err
	}

	objs.Objects = []Object{}
	for rows.Next() {
		var o Object
		if err := rows.Scan(&o.ID, &o.Type, &o.Constellation, &o.RA, &o.Dec, &o.RARadians, &o.DecRadians, &o.Magnitude, &o.Name, &o.M, &o.HourAngle, &o.Visible); err != nil {
			return objs, err
		}

		objs.Objects = append(objs.Objects, o)
	}

	return objs, nil
}

func visible(sel *sqrl.SelectBuilder) {
	t := time.Now()
	lst := mnt.LocalSiderealTime(t)
	lst = (lst / 24) * 360
	lat, _ := mnt.GetCoordinates()
	sel.Column("((dec_radians > ?) OR (cos(? - ra_radians) > (-1*tan(?)*tan(dec_radians)))) AS visible", mnt.Rad(90-lat), mnt.Rad(lst), mnt.Rad(lat))
}

func Visible(sel *sqrl.SelectBuilder) {
	sel.Where("visible IS TRUE")
}

func Named(sel *sqrl.SelectBuilder) {
	sel.Where("name IS NOT NULL")
}

func Messier(sel *sqrl.SelectBuilder) {
	sel.Where("m IS NOT NULL").
		OrderBy("m")
}

func Name(s string) QueryOption {
	return func(sel *sqrl.SelectBuilder) {
		sel.Where("name LIKE ?", fmt.Sprintf("%%%s%%", s))
	}
}

func Page(p, ps int) QueryOption {
	return func(sel *sqrl.SelectBuilder) {
		if ps > 0 {
			sel.Limit(uint64(ps)).Offset(uint64(p * ps))
		}

	}
}
