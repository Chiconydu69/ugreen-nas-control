package mqtt

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/fan"
)

// Config is the mqtt_* part of fan.cfg. The keys are read whatever cfg_version the file has and are never written
// back by this tool.
type Config struct {
	Enabled bool
	Host    string
	Port    int
	TLS     bool
	User    string
	Pass    string
	// Name is the part of the topics that tells this NAS from another one.
	Name     string
	Interval time.Duration
	// Control: restart and shut down. ControlUpdates: update plugins. ControlContainers: update containers.
	// All off unless switched on.
	Control           bool
	ControlUpdates    bool
	ControlContainers bool
}

var nameRe = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

func LoadConfig(path string) Config {
	v := fan.ReadValues(path)
	c := Config{
		Enabled:           v["mqtt_enabled"] == "1",
		Host:              v["mqtt_host"],
		Port:              1883,
		TLS:               v["mqtt_tls"] == "1",
		User:              v["mqtt_user"],
		Pass:              v["mqtt_pass"],
		Name:              nameRe.ReplaceAllString(v["mqtt_name"], ""),
		Interval:          5 * time.Second,
		Control:           v["mqtt_control"] == "1",
		ControlUpdates:    v["mqtt_control_updates"] == "1",
		ControlContainers: v["mqtt_control_containers"] == "1",
	}
	if c.TLS {
		c.Port = 8883
	}
	if n, err := strconv.Atoi(v["mqtt_port"]); err == nil && n > 0 && n < 65536 {
		c.Port = n
	}
	if n, err := strconv.Atoi(v["mqtt_interval"]); err == nil && n >= 2 && n <= 60 {
		c.Interval = time.Duration(n) * time.Second
	}
	if c.Name == "" {
		h, _ := os.Hostname()
		c.Name = nameRe.ReplaceAllString(strings.ToLower(h), "")
	}
	if c.Name == "" {
		c.Name = "nas"
	}
	return c
}
