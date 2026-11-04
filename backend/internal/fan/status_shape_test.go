package fan

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/fan/conflict"
)

// The `status` answer is what the web fixtures (web/models/fixture.ts) imitate. This test runs the real code over a
// matrix of models and settings and records the JSON types per top-level key, which keys are always there, and the
// keys inside the objects the page reads. testdata/status-shape.json is that record; web/src/testing/backend-shape.test.ts
// checks the fixtures against it. UNC_UPDATE_SHAPE=1 rewrites the file after an intended change of the answer.
const statusShapeFile = "testdata/status-shape.json"

type statusShape struct {
	Keys   map[string][]string `json:"keys"`   // top-level key -> JSON types seen
	Always []string            `json:"always"` // keys in every answer
	Nested map[string][]string `json:"nested"` // object (fans[] = each item) -> keys seen
}

func statusJSONType(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case float64:
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	}
	return "object"
}

func collectFanStatusShape(t *testing.T) statusShape {
	t.Helper()
	dir := isolate(t, "")
	oldCfg, oldState, oldLock, oldCalib, oldDet := configPath, statePath, lockPath, calibPath, detectConflicts
	oldHwmon, oldSys, oldINI := hwmonDir, sysDir, disksINI
	t.Cleanup(func() {
		configPath, statePath, lockPath, calibPath, detectConflicts = oldCfg, oldState, oldLock, oldCalib, oldDet
		hwmonDir, sysDir, disksINI = oldHwmon, oldSys, oldINI
	})
	configPath, statePath, lockPath = filepath.Join(dir, "fan.cfg"), filepath.Join(dir, "state.json"), filepath.Join(dir, "lock")
	calibPath = filepath.Join(dir, "fan_rpm.json")
	hwmonDir, sysDir, disksINI = filepath.Join(dir, "hwmon"), filepath.Join(dir, "sys"), filepath.Join(dir, "disks.ini")
	detectConflicts = func() []conflict.Conflict { return nil }

	types := map[string]map[string]bool{}
	nested := map[string]map[string]bool{}
	var answers []map[string]any
	add := func(m map[string]map[string]bool, k, v string) {
		if m[k] == nil {
			m[k] = map[string]bool{}
		}
		m[k][v] = true
	}
	setModel := func(model string) {
		if err := os.WriteFile(filepath.Join(dmiDir, "product_name"), []byte(model+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	fakeITEChip := func() (Fans, error) { return detectFans(newFakeITE(), productName()) }
	fakeECChip := func() (Fans, error) { return DetectEC(newFakeEC()) }

	m := FanMap{Version: 1, Model: "DXP4800 Plus", Chip: "IT8613", Outputs: []FanMapOutput{
		{ID: "fan2", Role: "cpu", PWM: "pwm2", Tach: "fan2"}, {ID: "fan3", Role: "case", PWM: "pwm3", Tach: "fan3"},
	}}
	cases := []struct {
		name   string
		model  string
		cfg    string
		fmap   bool
		open   func() (Fans, error)
		noChip bool // the answer carries chip_error instead of the chip
	}{
		{"fixed ITE, fan control on", "DXP6800 Pro", "", false, fakeITEChip, false},
		{"fixed EC, fan control on", "DXP4800 GT", "", false, fakeECChip, false},
		{"fan control off", "DXP6800 Pro", "fan_control=\"0\"\n", false, fakeITEChip, false},
		{"probe model without a map", "DXP4800 Plus", "", false, openChip, true},
		{"probe model with a hardware floor", "DXP2800 GT", "", false, openChip, true},
		{"probe model with a map", "DXP4800 Plus", "", true, fakeITEChip, false},
		{"model without fan control", "DX4600 Pro", "", false, openChip, true},
	}
	for _, c := range cases {
		for _, daemon := range []bool{false, true} {
			setModel(c.model)
			_ = os.Remove(fanMapPath)
			_ = os.Remove(statePath)
			if c.fmap {
				if err := writeJSONAtomic(fanMapPath, m); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(configPath, []byte(c.cfg), 0o644); err != nil {
				t.Fatal(err)
			}
			if daemon {
				b, _ := json.Marshal(State{Mode: "standard", Targets: map[string]int{"fan2": 30}})
				if err := os.WriteFile(statePath, b, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			b, err := json.Marshal(statusAnswer(c.open))
			if err != nil {
				t.Fatal(err)
			}
			var got map[string]any
			if err := json.Unmarshal(b, &got); err != nil {
				t.Fatalf("%s: %v", c.name, err)
			}
			if _, has := got["chip_error"]; has != c.noChip {
				t.Fatalf("%s: chip_error present = %v, want %v", c.name, has, c.noChip)
			}
			answers = append(answers, got)
			for k, v := range got {
				add(types, k, statusJSONType(v))
				switch x := v.(type) {
				case map[string]any:
					for nk := range x {
						add(nested, k, nk)
					}
				case []any:
					for _, item := range x {
						if o, ok := item.(map[string]any); ok && k != "conflicts" {
							for nk := range o {
								add(nested, k+"[]", nk)
							}
						}
					}
				}
			}
		}
	}
	var always []string
	for k := range types {
		all := true
		for _, a := range answers {
			if _, ok := a[k]; !ok {
				all = false
			}
		}
		if all {
			always = append(always, k)
		}
	}
	sort.Strings(always)
	flat := func(m map[string]map[string]bool) map[string][]string {
		o := map[string][]string{}
		for k, set := range m {
			for v := range set {
				o[k] = append(o[k], v)
			}
			sort.Strings(o[k])
		}
		return o
	}
	return statusShape{Keys: flat(types), Always: always, Nested: flat(nested)}
}

func TestFanStatusShapeMatchesRecord(t *testing.T) {
	got, err := json.MarshalIndent(collectFanStatusShape(t), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	if os.Getenv("UNC_UPDATE_SHAPE") == "1" {
		if err := os.WriteFile(filepath.FromSlash(statusShapeFile), got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(filepath.FromSlash(statusShapeFile))
	if err != nil {
		t.Fatal(err)
	}
	var a, b statusShape
	if err := json.Unmarshal(got, &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(want, &b); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) { // the file is formatted by bun fmt, so compare the content, not the bytes
		t.Errorf("the status answer changed; run UNC_UPDATE_SHAPE=1 go test ./internal/fan -run FanStatusShape and fix web/models/fixture.ts to match\n%s", strings.TrimSpace(fmt.Sprint(string(got))))
	}
}
