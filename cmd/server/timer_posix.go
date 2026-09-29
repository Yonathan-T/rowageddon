//go:build !windows

package main

import (
	"time"
)

type timeToken time.Time

func startPreciseTimer() timeToken {
	return timeToken(time.Now())
}

func elapsedPreciseMs(start timeToken) float64 {
	elapsed := time.Since(time.Time(start))
	return float64(elapsed.Nanoseconds()) / 1e6
}
