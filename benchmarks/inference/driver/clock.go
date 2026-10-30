package driver

import (
	"fmt"
	"time"
)

// maxResolution is the coarsest clock the driver accepts. The stub path's
// requests take tens to hundreds of microseconds, so anything coarser turns
// the percentiles into multiples of the clock tick.
const maxResolution = time.Microsecond

// Since returns the time elapsed from a Now() reading.
func Since(t0 int64) time.Duration { return time.Duration(Now() - t0) }

// Resolution is the smallest nonzero step the clock shows, measured.
func Resolution() time.Duration {
	best := time.Duration(1<<63 - 1)
	for range 10000 {
		a := Now()
		b := Now()
		for b == a {
			b = Now()
		}
		best = min(best, time.Duration(b-a))
	}
	return best
}

func checkClock() error {
	if r := Resolution(); r > maxResolution {
		return fmt.Errorf("driver: clock resolution is %s, coarser than the %s the measurements need", r, maxResolution)
	}
	return nil
}
