package mqtt

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/fan"
)

// Run is `ugreen-nas-fan publish`: it publishes until SIGTERM or SIGINT.
func Run() error {
	cfg := LoadConfig(fan.ConfigPath())
	if !cfg.Enabled {
		return fmt.Errorf("MQTT is switched off in the settings")
	}
	if cfg.Host == "" {
		return fmt.Errorf("no MQTT host in the settings")
	}

	prefix := "ugreen-nas/" + cfg.Name + "/"
	col := newCollector()
	cmdr := &commander{cfg: cfg, run: execRun, containers: dockerContainers, delay: time.Second,
		updates: func() []Update { return readUpdates(pluginsBoot, pluginsFresh, dockerStatus, unraidCheck) }}

	cl := &client{
		addr: fmt.Sprintf("%s:%d", cfg.Host, cfg.Port), useTLS: cfg.TLS, user: cfg.User, pass: cfg.Pass,
		clientID:  "ugreen-nas-" + cfg.Name,
		willTopic: prefix + "availability", willPayload: []byte("offline"),
	}
	last := map[string][]byte{}
	send := func(topic string, v any, retain bool) {
		b, err := json.Marshal(v)
		if err != nil {
			return
		}
		if retain && string(last[topic]) == string(b) {
			return
		}
		if cl.publish(prefix+topic, b, retain) == nil {
			last[topic] = b
		}
	}
	var lastMu sync.Mutex
	cl.onConnect = func() {
		lastMu.Lock()
		last = map[string][]byte{}
		lastMu.Unlock()
		_ = cl.publish(prefix+"availability", []byte("online"), true)
		if cfg.Control || cfg.ControlUpdates || cfg.ControlContainers {
			_ = cl.subscribe(prefix + "cmd")
		}
	}
	cmdr.report = func(r cmdResult) {
		b, _ := json.Marshal(r)
		_ = cl.publish(prefix+"cmd_result", b, false)
	}
	cl.onMessage = func(topic string, payload []byte, retained bool) {
		// The nonce map lives in memory only: a retained command would run again after every restart.
		if topic != prefix+"cmd" || retained {
			return
		}
		res, after := cmdr.handle(payload, time.Now())
		b, _ := json.Marshal(res)
		_ = cl.publish(prefix+"cmd_result", b, false)
		if after != nil {
			go after()
		}
	}

	stop := make(chan struct{})
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	go func() { <-sig; close(stop) }()
	runDone := make(chan struct{})
	go func() { cl.run(stop); close(runDone) }()

	tick := time.NewTicker(cfg.Interval)
	defer tick.Stop()
	var slowAt time.Time
	for {
		now := time.Now()
		lastMu.Lock()
		send("state", col.state(now), true)
		if now.Sub(slowAt) >= time.Minute || slowAt.IsZero() {
			slowAt = now
			send("updates", readUpdates(pluginsBoot, pluginsFresh, dockerStatus, unraidCheck), true)
		}
		send("notifications", readNotifications(notifyDir, now), true)
		lastMu.Unlock()

		select {
		case <-tick.C:
		case <-stop:
			// The client publishes the offline message itself before it disconnects (session).
			select {
			case <-runDone:
			case <-time.After(3 * time.Second):
			}
			return nil
		}
	}
}
