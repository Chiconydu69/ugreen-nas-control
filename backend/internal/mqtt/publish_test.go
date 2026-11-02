package mqtt

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/fan"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadConfig(t *testing.T) {
	p := filepath.Join(t.TempDir(), "fan.cfg")
	writeFile(t, p, "mqtt_enabled=\"1\"\nmqtt_host=\"broker\"\nmqtt_tls=\"1\"\nmqtt_name=\"My NAS!\"\nmqtt_interval=\"1\"\nmqtt_control=\"1\"\n")
	c := LoadConfig(p)
	if !c.Enabled || c.Host != "broker" || !c.TLS || c.Port != 8883 || c.Name != "MyNAS" || !c.Control || c.ControlUpdates || c.ControlContainers {
		t.Fatalf("config: %+v", c)
	}
	if c.Interval != 5*time.Second {
		t.Fatalf("interval below 2 s must keep the default, got %v", c.Interval)
	}
	if d := LoadConfig(filepath.Join(t.TempDir(), "none")); d.Enabled || d.Control || d.Port != 1883 || d.Name == "" {
		t.Fatalf("defaults: %+v", d)
	}
}

// A v1 fan.cfg (no cfg_version) with mqtt_* keys: the publisher reads them, the fan side ignores them, nothing is written.
func TestConfigFromV1File(t *testing.T) {
	p := filepath.Join(t.TempDir(), "fan.cfg")
	content := "mode=\"standard\"\nmqtt_enabled=\"1\"\nmqtt_host=\"10.0.0.2\"\nmqtt_port=\"1884\"\nmqtt_interval=\"10\"\nmqtt_control_updates=\"1\"\n"
	writeFile(t, p, content)
	c := LoadConfig(p)
	if !c.Enabled || c.Host != "10.0.0.2" || c.Port != 1884 || c.Interval != 10*time.Second || c.Control || !c.ControlUpdates {
		t.Fatalf("config: %+v", c)
	}
	if f := fan.LoadConfig(p); f.Version != 1 || len(f.Warnings) != 0 {
		t.Fatalf("fan side: version %d, warnings %v", f.Version, f.Warnings)
	}
	if b, _ := os.ReadFile(p); string(b) != content {
		t.Fatalf("fan.cfg was rewritten: %q", b)
	}
}

func TestReadNotifications(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 29, 20, 0, 0, 0, time.Local)
	writeFile(t, filepath.Join(dir, "a.notify"), "timestamp=\""+itoa(now.Add(-time.Hour).Unix())+"\"\nevent=\"Unraid\"\nsubject=\"Disk 3 hot\"\ndescription=\"57 C\"\nimportance=\"alert\"\n")
	writeFile(t, filepath.Join(dir, "b.notify"), "timestamp=\""+itoa(now.Add(-48*time.Hour).Unix())+"\"\nevent=\"Parity check finished\"\nimportance=\"normal\"\n")
	got := readNotifications(dir, now)
	if len(got) != 2 || got[0].Title != "Disk 3 hot" || got[0].Level != "alert" || got[0].Time != "19:00" || got[1].Title != "Parity check finished" || !strings.Contains(got[1].Time, "27.10.") {
		t.Fatalf("notifications: %+v", got)
	}
	if n := readNotifications(filepath.Join(dir, "none"), now); n == nil || len(n) != 0 {
		t.Fatalf("a missing directory must give an empty list, got %#v", n)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func TestVersionLess(t *testing.T) {
	for _, c := range []struct {
		a, b string
		less bool
	}{
		{"7.3.0", "7.3.1", true}, {"7.9.0", "7.10.0", true}, {"7.3.1", "7.3.0", false}, {"7.3.0", "7.3.0", false},
		{"2026.10.12", "2026.10.26", true}, {"2026.10.26", "2026.10.26a", true}, {"3.5", "3.6", true},
	} {
		if versionLess(c.a, c.b) != c.less {
			t.Errorf("versionLess(%q, %q) should be %v", c.a, c.b, c.less)
		}
	}
}

func TestReadUpdates(t *testing.T) {
	root := t.TempDir()
	boot, fresh := filepath.Join(root, "boot"), filepath.Join(root, "tmp")
	plg := func(v string) string {
		return "<?xml version='1.0'?>\n<!DOCTYPE PLUGIN [\n<!ENTITY name \"x\">\n<!ENTITY version \"" + v + "\">\n]>\n"
	}
	writeFile(t, filepath.Join(boot, "ugreen-nas-control.plg"), plg("3.5"))
	writeFile(t, filepath.Join(fresh, "ugreen-nas-control.plg"), plg("3.6"))
	writeFile(t, filepath.Join(boot, "unRAIDServer.plg"), plg("7.3.0"))
	writeFile(t, filepath.Join(fresh, "unRAIDServer.plg"), plg("7.3.1"))
	writeFile(t, filepath.Join(boot, "uptodate.plg"), plg("1.0"))
	writeFile(t, filepath.Join(fresh, "uptodate.plg"), plg("1.0"))
	writeFile(t, filepath.Join(boot, "nofresh.plg"), plg("1.0"))
	docker := filepath.Join(root, "docker.json")
	writeFile(t, docker, `{"lscr.io/plex:latest":{"status":"false"},"library/redis:latest":{"status":"false"},"mosquitto:latest":{"status":"true"},"x/y:1":{"status":"undef"}}`)

	osFile := filepath.Join(root, "result.json")
	writeFile(t, osFile, `{"version":"7.3.5","isNewer":true,"params":{"current_version":"7.3.4"}}`)
	got := readUpdates(boot, fresh, docker, osFile)
	want := []Update{
		{Kind: "system", Name: "Unraid OS", From: "7.3.4", To: "7.3.5"},
		{Kind: "plugin", Name: "ugreen-nas-control", File: "ugreen-nas-control.plg", From: "3.5", To: "3.6"},
		{Kind: "container", Name: "redis:latest", To: "new image"},
		{Kind: "container", Name: "lscr.io/plex:latest", To: "new image"},
	}
	if len(got) != len(want) {
		t.Fatalf("updates: %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("update %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestReadOSUpdate(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "result.json")
	writeFile(t, p, `{"version":"7.3.4","isNewer":false,"params":{"current_version":"7.3.4"}}`)
	if _, ok := readOSUpdate(p); ok {
		t.Fatal("isNewer false must give no update")
	}
	if _, ok := readOSUpdate(filepath.Join(dir, "none")); ok {
		t.Fatal("a missing file must give no update")
	}
	writeFile(t, p, "not json")
	if _, ok := readOSUpdate(p); ok {
		t.Fatal("garbage must give no update")
	}
	// Without the result of Unraid's check, an installed unRAIDServer.plg still tells.
	boot, fresh := filepath.Join(dir, "boot"), filepath.Join(dir, "fresh")
	plg := func(v string) string { return "<!ENTITY version   \"" + v + "\">\n" }
	writeFile(t, filepath.Join(boot, "unRAIDServer.plg"), plg("7.3.0"))
	writeFile(t, filepath.Join(fresh, "unRAIDServer.plg"), plg("7.3.1"))
	if u := readUpdates(boot, fresh, filepath.Join(dir, "none"), filepath.Join(dir, "none")); len(u) != 1 || u[0].Kind != "system" || u[0].To != "7.3.1" {
		t.Fatalf("fallback: %+v", u)
	}
}

func TestReadNetCounters(t *testing.T) {
	p := filepath.Join(t.TempDir(), "dev")
	writeFile(t, p, "Inter-|   Receive |  Transmit\n face |bytes packets errs drop fifo frame compressed multicast|bytes packets\n  eth0: 1000 5 0 0 0 0 0 0 2000 7 0 0 0 0 0 0\n    lo: 1 1 0 0 0 0 0 0 1 1 0 0 0 0 0 0\n")
	m := readNetCounters(p)
	if m["eth0"] != (netCounters{1000, 2000}) || len(m) != 2 {
		t.Fatalf("counters: %+v", m)
	}
}

func ip(n int) *int { return &n }

func TestBuildAlarms(t *testing.T) {
	cfg := fan.LoadConfig(filepath.Join(t.TempDir(), "none"))
	bays := []fan.Bay{{Bay: 1, Present: true, HDD: true, Temp: ip(40)}, {Bay: 3, Present: true, HDD: true, Temp: ip(57)}, {Bay: 4, Present: true, HDD: true, Standby: true, Temp: ip(80)}}
	a := buildAlarms(ip(50), bays, nil, nil, cfg)
	if len(a) != 1 || a[0].Code != "disk_hot" || a[0].Bay != 3 || *a[0].Value != 57 || a[0].Limit != 55 {
		t.Fatalf("alarms: %+v", a)
	}
	st := &fan.State{Failsafe: "disk_hot", Stalled: []string{"fan3"}}
	a = buildAlarms(ip(50), bays, nil, st, cfg)
	if len(a) != 3 || a[0].Hint == "" || a[2].Code != "fan_stalled" {
		t.Fatalf("with fail-safe: %+v", a)
	}
	if a := buildAlarms(ip(50), nil, nil, nil, cfg); a == nil || len(a) != 0 {
		t.Fatalf("no alarm must be an empty list, got %#v", a)
	}
	if a := buildAlarms(ip(95), nil, nil, nil, cfg); len(a) != 1 || a[0].Code != "cpu_hot" {
		t.Fatalf("cpu: %+v", a)
	}
}

func cmdJSON(c command) []byte { b, _ := json.Marshal(c); return b }

type runLog struct {
	mu    sync.Mutex
	calls [][]string
}

func (r *runLog) run(name string, args ...string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, append([]string{name}, args...))
	return nil
}

func TestCommander(t *testing.T) {
	now := time.Now()
	rl := &runLog{}
	c := &commander{
		cfg: Config{Control: true, ControlUpdates: true}, run: rl.run,
		updates: func() []Update {
			return []Update{{Kind: "plugin", Name: "ugreen-nas-control", File: "ugreen-nas-control.plg"}, {Kind: "plugin", Name: "evil", File: "../x.plg"}, {Kind: "plugin", Name: "-rf", File: "-rf.plg"}}
		},
		containers: func(string) ([]string, error) {
			t.Fatal("containers must not be looked up while switched off")
			return nil, nil
		},
	}
	check := func(cmd command, at time.Time, ok bool, text string) func() {
		t.Helper()
		res, after := c.handle(cmdJSON(cmd), at)
		if res.OK != ok || (text != "" && !strings.Contains(res.Text, text)) || res.Nonce != cmd.Nonce || (ok != (after != nil)) {
			t.Fatalf("%+v: result %+v, after set %v", cmd, res, after != nil)
		}
		return after
	}

	after := check(command{Cmd: "reboot", TS: now.Unix(), Nonce: "n1"}, now, true, "started")
	after()
	if len(rl.calls) != 1 || rl.calls[0][1] != "-r" {
		t.Fatalf("reboot must call powerdown -r, got %v", rl.calls)
	}
	check(command{Cmd: "reboot", TS: now.Unix(), Nonce: "n1"}, now.Add(5*time.Second), false, "already seen")
	check(command{Cmd: "shutdown", TS: now.Unix(), Nonce: "n2"}, now, false, "too fast")
	later := now.Add(20 * time.Second)
	after = check(command{Cmd: "shutdown", TS: later.Unix(), Nonce: "n3"}, later, true, "")
	after()
	if len(rl.calls) != 2 || len(rl.calls[1]) != 1 {
		t.Fatalf("shutdown must call powerdown without arguments, got %v", rl.calls)
	}
	l2 := later.Add(20 * time.Second)
	check(command{Cmd: "shutdown", TS: now.Unix(), Nonce: "n4"}, l2, false, "too old")
	check(command{Cmd: "format", TS: l2.Unix(), Nonce: "n5"}, l2, false, "unknown")
	check(command{Cmd: "reboot", TS: l2.Unix()}, l2, false, "nonce")
	check(command{Cmd: "update", Target: "container", Name: "x", TS: l2.Unix(), Nonce: "n6"}, l2, false, "container updates from the display are switched off")
	check(command{Cmd: "update", Target: "volume", Name: "x", TS: l2.Unix(), Nonce: "n6b"}, l2, false, "only plugin and container")
	check(command{Cmd: "update", Target: "plugin", Name: "nothing", TS: l2.Unix(), Nonce: "n7"}, l2, false, "no such update")
	check(command{Cmd: "update", Target: "plugin", Name: "evil", TS: l2.Unix(), Nonce: "n8"}, l2, false, "no such update")
	check(command{Cmd: "update", Target: "plugin", Name: "-rf", TS: l2.Unix(), Nonce: "n8b"}, l2, false, "no such update")
	after = check(command{Cmd: "update", Target: "plugin", Name: "ugreen-nas-control", TS: l2.Unix(), Nonce: "n9"}, l2, true, "")
	after()
	if got := rl.calls[2]; len(got) != 3 || got[1] != "update" || got[2] != "ugreen-nas-control.plg" {
		t.Fatalf("update call: %v", got)
	}
	if res, _ := c.handle([]byte("not json"), l2); res.OK {
		t.Fatal("garbage must be rejected")
	}
}

func TestCommanderContainers(t *testing.T) {
	now := time.Now()
	rl := &runLog{}
	c := &commander{
		cfg: Config{ControlContainers: true}, run: rl.run,
		updates: func() []Update {
			return []Update{{Kind: "container", Name: "ghcr.io/x/homarr:latest"}, {Kind: "container", Name: "evil:1"}, {Kind: "container", Name: "two:1"}, {Kind: "container", Name: "gone:1"}}
		},
		containers: func(image string) ([]string, error) {
			switch image {
			case "ghcr.io/x/homarr:latest":
				return []string{"homarr"}, nil
			case "evil:1":
				return []string{"a; reboot"}, nil
			case "two:1":
				return []string{"two-a", "two-b"}, nil
			}
			return nil, nil
		},
	}
	at := now
	check := func(cmd command, ok bool, text string) func() {
		t.Helper()
		at = at.Add(20 * time.Second)
		cmd.TS = at.Unix()
		res, after := c.handle(cmdJSON(cmd), at)
		if res.OK != ok || (text != "" && !strings.Contains(res.Text, text)) || (ok != (after != nil)) {
			t.Fatalf("%+v: result %+v, after set %v", cmd, res, after != nil)
		}
		return after
	}
	check(command{Cmd: "update", Target: "plugin", Name: "p", Nonce: "c1"}, false, "updates from the display are switched off")
	check(command{Cmd: "reboot", Nonce: "c2"}, false, "switched off")
	check(command{Cmd: "update", Target: "container", Name: "not-in-list:1", Nonce: "c3"}, false, "no such update")
	check(command{Cmd: "update", Target: "container", Name: "gone:1", Nonce: "c4"}, false, "no container found")
	check(command{Cmd: "update", Target: "container", Name: "evil:1", Nonce: "c5"}, false, "not allowed")
	after := check(command{Cmd: "update", Target: "container", Name: "ghcr.io/x/homarr:latest", Nonce: "c6"}, true, "")
	after()
	if len(rl.calls) != 1 || rl.calls[0][0] != updateContainerBin || rl.calls[0][1] != "homarr" || len(rl.calls[0]) != 2 {
		t.Fatalf("container update call: %v", rl.calls)
	}
	after = check(command{Cmd: "update", Target: "container", Name: "two:1", Nonce: "c7"}, true, "")
	after()
	if len(rl.calls) != 3 || rl.calls[1][1] != "two-a" || rl.calls[2][1] != "two-b" {
		t.Fatalf("one update per container expected: %v", rl.calls)
	}
}

func TestCommanderAllContainers(t *testing.T) {
	now := time.Now()
	var got []cmdResult
	var mu sync.Mutex
	failOn := "b"
	c := &commander{
		cfg: Config{ControlContainers: true},
		run: func(_ string, args ...string) error {
			if len(args) == 1 && args[0] == failOn {
				return errors.New("pull failed")
			}
			return nil
		},
		report: func(r cmdResult) { mu.Lock(); got = append(got, r); mu.Unlock() },
		updates: func() []Update {
			return []Update{{Kind: "container", Name: "img-a"}, {Kind: "plugin", Name: "p", File: "p.plg"}, {Kind: "container", Name: "img-b"}, {Kind: "container", Name: "img-c"}, {Kind: "container", Name: "img-none"}}
		},
		containers: func(image string) ([]string, error) {
			return map[string][]string{"img-a": {"a"}, "img-b": {"b"}, "img-c": {"c1", "c2"}}[image], nil
		},
	}
	res, after := c.handle(cmdJSON(command{Cmd: "update", Target: "containers", TS: now.Unix(), Nonce: "x1"}), now)
	if !res.OK || after == nil {
		t.Fatalf("update of all containers: %+v", res)
	}
	if r2, a2 := c.handle(cmdJSON(command{Cmd: "update", Target: "containers", TS: now.Add(15 * time.Second).Unix(), Nonce: "x2"}), now.Add(15*time.Second)); r2.OK || a2 != nil || !strings.Contains(r2.Text, "busy") {
		t.Fatalf("a second command must wait while the first runs: %+v", r2)
	}
	after()
	if len(got) != 6 {
		t.Fatalf("running per step (4), one failure and the end: %+v", got)
	}
	steps := ""
	for _, r := range got {
		steps += r.Name + ":" + r.Text + ","
	}
	if !strings.Contains(steps, "a:running,") || !strings.Contains(steps, "b:failed: pull failed,") || !strings.Contains(steps, "c2:running,") {
		t.Fatalf("steps: %s", steps)
	}
	last := got[len(got)-1]
	if !last.Done || last.OK || last.Step != 4 || last.Total != 4 || last.Nonce != "x1" || last.Text != "1 of 4 failed" {
		t.Fatalf("last answer: %+v", last)
	}
	failOn = ""
	later := now.Add(40 * time.Second)
	res, after = c.handle(cmdJSON(command{Cmd: "update", Target: "containers", TS: later.Unix(), Nonce: "x3"}), later)
	if !res.OK {
		t.Fatalf("after the first run the next command may start: %+v", res)
	}
	after()
	if last := got[len(got)-1]; !last.Done || !last.OK || last.Text != "done" {
		t.Fatalf("clean run: %+v", last)
	}
}

func TestMatchContainers(t *testing.T) {
	list := "homarr\tghcr.io/homarr-labs/homarr:latest\nweb\tnginx\nweb2\tlibrary/nginx\nother\tredis:7\nwp\twordpress\nbs\tlscr.io/linuxserver/bookstack\nnc\tnextcloud:latest\nreg\tlocalhost:5000/app\n"
	cases := []struct {
		image string
		want  string
	}{
		{"ghcr.io/homarr-labs/homarr:latest", "homarr"},
		{"nginx:latest", "web web2"},
		{"wordpress:latest", "wp"},
		{"docker.io/library/wordpress:latest", "wp"},
		{"lscr.io/linuxserver/bookstack:latest", "bs"},
		{"nextcloud", "nc"},
		{"redis:7", "other"},
		{"redis:latest", ""},
		{"localhost:5000/app:latest", "reg"},
		{"nope", ""},
	}
	for _, c := range cases {
		if got := strings.Join(matchContainers(list, c.image), " "); got != c.want {
			t.Errorf("%s: got %q, want %q", c.image, got, c.want)
		}
	}
}

func TestCommanderOff(t *testing.T) {
	now := time.Now()
	c := &commander{cfg: Config{}, run: (&runLog{}).run, updates: func() []Update { return nil }}
	for i, cmd := range []string{"reboot", "shutdown", "update", "container"} {
		target := "plugin"
		if cmd == "container" {
			cmd, target = "update", "container"
		}
		res, after := c.handle(cmdJSON(command{Cmd: cmd, Target: target, TS: now.Unix(), Nonce: "n" + string(rune('a'+i))}), now)
		if res.OK || after != nil || !strings.Contains(res.Text, "switched off") {
			t.Fatalf("%s must be refused while remote control is off: %+v", cmd, res)
		}
	}
}

// A broker that does just enough: CONNACK, records PUBLISH, answers SUBSCRIBE, and sends one message to the subscriber.
func TestMQTTClient(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	got := make(chan string, 10)
	connect := make(chan []byte, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		typ, _, body, err := readPacket(r)
		if err != nil || typ != mqttConnect {
			return
		}
		connect <- body
		conn.Write([]byte{mqttConnack << 4, 2, 0, 0})
		for {
			typ, flags, body, err := readPacket(r)
			if err != nil {
				return
			}
			switch typ {
			case mqttPublish:
				tl := int(body[0])<<8 | int(body[1])
				got <- string(body[2:2+tl]) + "=" + string(body[2+tl:]) + "|retain=" + string('0'+rune(flags&1))
			case mqttSubscribe:
				conn.Write([]byte{mqttSuback << 4, 3, body[0], body[1], 1})
				conn.Write(publishPacket("a/cmd", []byte(`{"x":1}`), false))
				conn.Write(publishPacket("a/cmd", []byte(`{"x":2}`), true))
			}
		}
	}()

	received := make(chan string, 2)
	c := &client{addr: ln.Addr().String(), user: "u", pass: "p", clientID: "id", willTopic: "a/availability", willPayload: []byte("offline")}
	c.onConnect = func() {
		_ = c.publish("a/state", []byte("hello"), true)
		_ = c.subscribe("a/cmd")
	}
	c.onMessage = func(topic string, p []byte, retained bool) {
		received <- topic + "=" + string(p) + "|retained=" + string('0'+rune(map[bool]int{false: 0, true: 1}[retained]))
	}
	stop := make(chan struct{})
	defer close(stop)
	go c.run(stop)

	select {
	case body := <-connect:
		for _, want := range []string{"MQTT", "a/availability", "offline", "u", "p", "id"} {
			if !strings.Contains(string(body), want) {
				t.Errorf("CONNECT lacks %q: %q", want, body)
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no CONNECT")
	}
	select {
	case m := <-got:
		if m != "a/state=hello|retain=1" {
			t.Fatalf("publish: %q", m)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no PUBLISH")
	}
	select {
	case m := <-received:
		if m != `a/cmd={"x":1}|retained=0` {
			t.Fatalf("received: %q", m)
		}
		if m := <-received; m != `a/cmd={"x":2}|retained=1` {
			t.Fatalf("retained flag lost: %q", m)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no message received")
	}
}

func TestEncodeLength(t *testing.T) {
	for n, want := range map[int][]byte{0: {0}, 127: {127}, 128: {0x80, 1}, 16383: {0xff, 0x7f}, 16384: {0x80, 0x80, 1}} {
		if got := encodeLength(n); string(got) != string(want) {
			t.Errorf("encodeLength(%d) = %v, want %v", n, got, want)
		}
	}
}

func TestCollectorState(t *testing.T) {
	dir := t.TempDir()
	counters := filepath.Join(dir, "dev")
	old := procNetDev
	t.Cleanup(func() { procNetDev = old })
	procNetDev = counters

	cfgFile := filepath.Join(dir, "none")
	c := &collector{
		live: func() fan.Live {
			return fan.Live{Sensors: fan.Sensors{CPU: ip(50)}, Info: fan.Info{Unraid: "7.3.0"}, FanControl: true, Fans: []fan.FanStatus{{ID: "pwm1", Role: "cpu", RPM: 900}}}
		},
		hardware: func() fan.Hardware {
			return fan.Hardware{Model: "DXP4800", Net: []fan.NetPort{{Name: "eth0", Link: true, SpeedMbit: 1000}}}
		},
		settings: func() fan.Config { return fan.LoadConfig(cfgFile) },
		daemon:   func(time.Time) *fan.State { return &fan.State{Mode: "quiet", CaseTemp: ip(33)} },
	}
	now := time.Now()
	writeFile(t, counters, "  eth0: 1000 5 0 0 0 0 0 0 2000 7 0 0 0 0 0 0\n")
	c.state(now)
	writeFile(t, counters, "  eth0: 6000 5 0 0 0 0 0 0 4000 7 0 0 0 0 0 0\n")
	s := c.state(now.Add(5 * time.Second))
	if s.Model != "DXP4800" || s.Mode != "quiet" || *s.CaseTemp != 33 || len(s.Fans) != 1 || s.Fans[0].RPM != 900 || s.Bays == nil || s.Alarms == nil {
		t.Fatalf("state: %+v", s)
	}
	if len(s.Net) != 1 || s.Net[0].RxBps != 1000 || s.Net[0].TxBps != 400 {
		t.Fatalf("net rates: %+v", s.Net)
	}
}

func TestMQTTClientStopEndsSessionAtOnce(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	disconnect := make(chan bool, 1)
	up := make(chan struct{})
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		if _, _, _, err := readPacket(r); err != nil {
			return
		}
		conn.Write([]byte{mqttConnack << 4, 2, 0, 0})
		close(up)
		typ, _, _, err := readPacket(r) // silent broker: only a DISCONNECT or a close ends this
		disconnect <- err == nil && typ == mqttDisconn
	}()
	c := &client{addr: ln.Addr().String(), clientID: "id"}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() { c.run(stop); close(done) }()
	select {
	case <-up:
	case <-time.After(5 * time.Second):
		t.Fatal("no connection")
	}
	time.Sleep(50 * time.Millisecond)
	close(stop)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("run kept the connection after stop")
	}
	if !<-disconnect {
		t.Error("no DISCONNECT sent")
	}
}

func TestCommanderNonceOnlyUsedWhenAccepted(t *testing.T) {
	now := time.Now()
	rl := &runLog{}
	c := &commander{cfg: Config{}, run: rl.run}
	cmd := command{Cmd: "reboot", TS: now.Unix(), Nonce: "x1"}
	if res, _ := c.handle(cmdJSON(cmd), now); res.OK || !strings.Contains(res.Text, "switched off") {
		t.Fatalf("off: %+v", res)
	}
	c.cfg.Control = true
	res, after := c.handle(cmdJSON(cmd), now)
	if !res.OK || after == nil {
		t.Fatalf("a refused command burned its nonce: %+v", res)
	}
	// Not finished yet: the next one waits, even after the minimum spacing.
	next := now.Add(cmdMinSpace + time.Second)
	if res, _ := c.handle(cmdJSON(command{Cmd: "reboot", TS: next.Unix(), Nonce: "x2"}), next); res.OK || !strings.Contains(res.Text, "still running") {
		t.Fatalf("busy: %+v", res)
	}
	after()
	if res, _ := c.handle(cmdJSON(command{Cmd: "reboot", TS: next.Unix(), Nonce: "x2"}), next); !res.OK {
		t.Fatalf("after finishing: %+v", res)
	}
}

func TestMQTTClientStopPublishesOfflineBeforeDisconnect(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	got := make(chan []string, 1)
	up := make(chan struct{})
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		if _, _, _, err := readPacket(r); err != nil {
			return
		}
		conn.Write([]byte{mqttConnack << 4, 2, 0, 0})
		close(up)
		var seen []string
		for {
			typ, _, body, err := readPacket(r)
			if err != nil {
				break
			}
			switch typ {
			case mqttPublish:
				tl := int(body[0])<<8 | int(body[1])
				seen = append(seen, string(body[2+tl:]))
			case mqttDisconn:
				seen = append(seen, "DISCONNECT")
			}
			if typ == mqttDisconn {
				break
			}
		}
		got <- seen
	}()
	c := &client{addr: ln.Addr().String(), clientID: "id", willTopic: "t/availability", willPayload: []byte("offline")}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() { c.run(stop); close(done) }()
	select {
	case <-up:
	case <-time.After(5 * time.Second):
		t.Fatal("no connection")
	}
	close(stop)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("run kept the connection after stop")
	}
	select {
	case seen := <-got:
		if len(seen) != 2 || seen[0] != "offline" || seen[1] != "DISCONNECT" {
			t.Errorf("broker saw %v, want [offline DISCONNECT]", seen)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("broker saw nothing")
	}
}
