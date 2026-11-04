package fan

import (
	"encoding/json"
	"os"
	"time"
)

// What the MQTT publisher (internal/mqtt) reads of this package. All of it only reads, the way `status` does.

// ConfigPath is the fan.cfg the settings are read from.
func ConfigPath() string { return configPath }

// ReadValues reads a key="value" file such as fan.cfg or an Unraid .notify file.
func ReadValues(path string) map[string]string { return readValues(path) }

// Settings is fan.cfg as the daemon decides with it: the emergency limits are lowered to what the model needs.
func Settings() Config { return ApplyModelLimits(LoadConfig(configPath), productName()) }

// Live is the status without the parts the page adds: the readings, and the fans while fan control is on.
type Live struct {
	Sensors    Sensors
	Info       Info
	Fans       []FanStatus
	FanControl bool
}

// ReadLive reads the fan chip only while fan control is on, as `status` does.
func ReadLive() Live {
	l := Live{Sensors: ReadSensors(), Info: ReadInfo(), FanControl: LoadConfig(configPath).FanControl}
	if l.FanControl {
		out := map[string]any{}
		addChipStatus(out, &l.Sensors, openChipOnce)
		l.Fans, _ = out["fans"].([]FanStatus)
	}
	return l
}

// ReadDaemonState is the state file of the running daemon, nil when it is missing or older than 30 s.
func ReadDaemonState(now time.Time) *State {
	b, err := os.ReadFile(statePath)
	if err != nil {
		return nil
	}
	var s State
	if json.Unmarshal(b, &s) != nil || now.Unix()-s.Time >= 30 {
		return nil
	}
	return &s
}
