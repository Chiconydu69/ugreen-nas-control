package fan

import (
	"strings"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/hw"
)

// Fans is a fan controller as the commands and the daemon see it: the
// IT8613E Super-I/O chip of the DXP6800 Pro or the IT5571 embedded controller
// of the DXP4800 GT. Callers hold the lock around every call.
type Fans interface {
	Name() string                                  // IT8613, IT5571
	Describe() string                              // for the log: where the chip sits
	Details() map[string]any                       // extra fields of probe
	Outputs() []Channel                            // fan headers in use
	MaxDuty() byte                                 // duty value of 100 %
	AutoDutyKnown() bool                           // duty register tells the real output in auto mode
	RPM(Channel) (int, error)                      // 0 when it stands or nothing is connected
	PWM(Channel) (duty byte, auto bool, err error) // auto: the controller's own curve drives it
	SetPWM(Channel, byte) error                    // switches to manual if needed
	Temps() []*int                                 // the controller's own sensors in °C; nil where none
	Snapshot() (Snapshot, error)                   // state to give back when the plugin lets go
	Restore(Snapshot) error
}

// BoardTemper is implemented by controllers that read the mainboard temperature.
type BoardTemper interface {
	BoardTemp() *int
}

type Channel struct {
	ID       string // fan2, fan3, ...
	TachLow  byte
	TachHigh byte
	Duty     byte
	Ctrl     byte
}

// Saved state of all outputs, written back when the plugin lets go.
type Snapshot map[string][2]byte // channel id -> {ctrl, duty}

func productName() string {
	return hw.ProductName(dmiDir)
}

func isDXP4800GT(model string) bool {
	return strings.HasPrefix(model, "DXP4800 GT")
}

// detectFans picks the controller by model: the DXP4800 GT and the iDX (Pro or plain, the plain one read only) have
// no Super-I/O fan chip, their fans hang on the embedded controller; the DXP2800 GT has a Chipsea CS201x, gated by
// `cs201x confirm`. On the IT8613E models without a known wiring the channels come from the confirmed
// fan map; without one the chip comes with the probe candidates (pwm1..pwm4), which is enough to hand the fans
// back (`restore`) but never reaches the daemon: requireSupportedModel stops that first.
func detectFans(p Ports, model string) (Fans, error) {
	switch {
	case isDXP4800GT(model):
		return DetectEC(p)
	case isIdxPro(model):
		return detectEC(p, ecSpecIdxPro)
	case isIdxPlain(model):
		return detectEC(p, ecSpecIdx)
	case isDXP2800GT(model):
		return DetectCS201x(p, model)
	case FanKind(model) == KindProbe:
		c, err := Detect(p)
		if err != nil {
			return nil, err
		}
		c.floor = percentToDuty(probeFloorPct, 255)
		if m, err := loadFanMap(model); err == nil {
			c.Channels = m.channels()
		} else {
			c.Channels = iteCandidates()
		}
		return c, nil
	}
	return Detect(p)
}
