package main

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestHourAngle(t *testing.T) {
	lon = 104.99
	ts := time.Date(2025, 10, 21, 11, 42, 0, 0, time.UTC)
	testCases := []struct {
		ra string
		ha float64
	}{
		{
			ra: "12:38.4",
			ha: 1,
		},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("%02d", i), func(t *testing.T) {
			ha, err := hourAngle(tc.ra, ts)
			assert.NoError(t, err)
			assert.Equal(t, tc.ha, ha)
		})
	}
}
