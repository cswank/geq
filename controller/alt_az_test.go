package main_test

import (
	"fmt"
	"testing"
)

func TestAltAz(t *testing.T) {
	testCases := []struct {
		ra  string
		dec string
		az  float64
		alt float64
		ts  float64
		lat float64
		lon float64
	}{
		{
			ra:  "17:53.9",
			dec: "-34:49",
			ts:  1745453263,
			lat: 39.735310,
			lon: -104.793277,
		},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("%02d", i), func(t *testing.T) {
		})
	}
}
