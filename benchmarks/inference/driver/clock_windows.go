package driver

import (
	"syscall"
	"time"
	"unsafe"
)

// Go's monotonic clock on Windows advances with the system timer interrupt,
// about every 0.5 ms, so time.Since reads 0 for a sub-millisecond request.
// QueryPerformanceCounter ticks at 10 MHz (100 ns) and costs about 60 ns a
// call, so the driver reads it directly here.
var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	qpc      = kernel32.NewProc("QueryPerformanceCounter")
	qpcFreq  = func() int64 {
		var f int64
		_, _, _ = kernel32.NewProc("QueryPerformanceFrequency").Call(uintptr(unsafe.Pointer(&f)))
		return f
	}()
)

// Now returns a monotonic reading in nanoseconds.
func Now() int64 {
	var c int64
	_, _, _ = qpc.Call(uintptr(unsafe.Pointer(&c)))
	// Split to avoid overflowing c * 1e9 after about 15 minutes of uptime at 10 MHz.
	return c/qpcFreq*int64(time.Second) + c%qpcFreq*int64(time.Second)/qpcFreq
}
