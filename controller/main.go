package main

import (
	"log"

	"github.com/alecthomas/kingpin/v2"
	"github.com/cswank/geq/controller/internal/mount"
	"github.com/cswank/geq/controller/internal/server"
)

var (
	dev = kingpin.Flag("serial", "serial device").String()
	lon = kingpin.Flag("longitude", "longitude").Float64()
)

func main() {
	kingpin.Parse()

	m, err := mount.New(*dev, *lon, 23, 24)
	if err != nil {
		log.Fatal(err)
	}

	s, err := server.New(m)
	if err != nil {
		log.Fatal(err)
	}

	if err := s.Start(); err != nil {
		log.Fatal(err)
	}
}
