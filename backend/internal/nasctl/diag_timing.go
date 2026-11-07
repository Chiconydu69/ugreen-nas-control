package nasctl

import (
	"fmt"
	"os"
	"time"
)

// timingOn: UNC_DIAG_TIMING=1 prints how long each diag section and tool call took to stderr (for profiling).
var timingOn = os.Getenv("UNC_DIAG_TIMING") == "1"

func timed(kind, name string) func() {
	if !timingOn {
		return func() {}
	}
	t := time.Now()
	return func() {
		fmt.Fprintf(os.Stderr, "diag-timing: %-7s %7.0f ms  %s\n", kind, float64(time.Since(t).Microseconds())/1000, name)
	}
}
