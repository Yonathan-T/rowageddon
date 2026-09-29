//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

var (
	modkernel32 = syscall.NewLazyDLL("kernel32.dll")
	procQPC     = modkernel32.NewProc("QueryPerformanceCounter")
	procQPF     = modkernel32.NewProc("QueryPerformanceFrequency")
	qpcFreq     = func() float64 {
		var freq int64
		procQPF.Call(uintptr(unsafe.Pointer(&freq)))
		if freq == 0 {
			return 10000000.0
		}
		return float64(freq)
	}()
)

type timeToken int64

func startPreciseTimer() timeToken {
	var qpc int64
	procQPC.Call(uintptr(unsafe.Pointer(&qpc)))
	return timeToken(qpc)
}

func elapsedPreciseMs(start timeToken) float64 {
	var end int64
	procQPC.Call(uintptr(unsafe.Pointer(&end)))
	elapsedSec := float64(end-int64(start)) / qpcFreq
	return elapsedSec * 1000.0
}
