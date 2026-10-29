//go:build !windows

package driver

import "time"

var epoch = time.Now()

// Now returns a monotonic reading in nanoseconds.
func Now() int64 { return int64(time.Since(epoch)) }
