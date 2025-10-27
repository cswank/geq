package main

import (
	"log"

	"github.com/alecthomas/kingpin/v2"
	"github.com/cswank/geq/controller/internal/mount"
	"github.com/cswank/geq/controller/internal/server"
)

var (
	serial = kingpin.Flag("serial", "serial device").String()
	lon    = kingpin.Flag("longitude", "longitude").Float64()
	dev    = kingpin.Flag("dev", "develpment mode (no mount)").Short('d').Bool()
)

func main() {
	kingpin.Parse()

	var err error
	var m *mount.TelescopeMount
	if !*dev {
		m, err = mount.New(*serial, *lon, 23, 24)
		if err != nil {
			log.Fatal(err)
		}
	}

	s, err := server.New(m)
	if err != nil {
		log.Fatal(err)
	}

	if err := s.Start(); err != nil {
		log.Fatal(err)
	}
}
