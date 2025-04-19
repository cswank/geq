package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"log"
	"time"

	"go.bug.st/serial.v1"
)

type motor struct {
	RPM       float64
	Steps     int32
	Microstep uint8
}

type job struct {
	M1 motor
	M2 motor
}

func main() {
	mode := &serial.Mode{
		BaudRate: 115200,
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	}

	port, err := serial.Open("/dev/serial0", mode)
	if err != nil {
		log.Fatal(err)
		return
	}

	defer port.Close()

	i := int32(1)
	j := 0
	rpm := []float64{
		400,
		25,
	}

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

	for {
		var buf bytes.Buffer
		jb := job{
			M1: motor{
				RPM:       rpm[j%2],
				Steps:     1000 * i,
				Microstep: 16,
			},
			M2: motor{
				RPM:       rpm[j%2],
				Steps:     10000 * i,
				Microstep: 16,
			},
		}

		i *= -1
		j += 1
		if j == 16 {
			j = 0
		}

		binary.Write(&buf, binary.LittleEndian, jb)

		data := buf.Bytes()

		fmt.Printf("%X\n", data)

		_, err = port.Write(data)
		if err != nil {
			log.Fatal(err)
		}

		time.Sleep(5 * time.Second)
	}
}
