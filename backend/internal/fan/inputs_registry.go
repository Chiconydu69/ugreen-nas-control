package fan

import (
	"fmt"
	"log/syslog"
	"os"
	"sync"
)

// daemonInputs is the input registry of the daemon process: it lives across ticks, so the script and nvidia-smi
// answers keep their 10 s TTL, and every tick starts a fresh per-tick cache from that tick's sensors.
var (
	daemonInputs     *Registry
	daemonInputsOnce sync.Once
)

func init() {
	newInputSource = func(c Config, s Sensors) InputSource {
		daemonInputsOnce.Do(func() { daemonInputs = NewRegistry(syslogf) })
		daemonInputs.begin(c, s)
		return daemonInputs
	}
}

var (
	syslogOnce   sync.Once
	syslogWriter *syslog.Writer
)

func syslogf(format string, args ...any) {
	syslogOnce.Do(func() { syslogWriter, _ = syslog.New(syslog.LOG_INFO|syslog.LOG_DAEMON, "ugreen-nas-control") })
	msg := fmt.Sprintf(format, args...)
	if syslogWriter != nil {
		_ = syslogWriter.Info(msg)
		return
	}
	fmt.Fprintln(os.Stderr, msg)
}
