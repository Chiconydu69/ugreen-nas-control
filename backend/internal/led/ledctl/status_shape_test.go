package ledctl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// The `status` answer is what the web fixtures (web/models/fixture.ts) imitate. This test runs the real code over a
// matrix of products and settings and records, per top-level key, the JSON types it emits, and for enum-valued keys
// the values it emits. testdata/status-shape.json is that record; web/src/testing/backend-shape.test.ts checks
// the fixtures against it. UNC_UPDATE_SHAPE=1 rewrites the file after an intended change of the answer.
const shapeFile = "testdata/status-shape.json"

type statusShape struct {
	Keys  map[string][]string `json:"keys"`  // key -> JSON types seen
	Enums map[string][]string `json:"enums"` // key -> values seen
}

// enumKeys: strings with a closed set of values.
var enumKeys = []string{"mapping", "mode", "netdev_status", "shutdown_effect", "i2c_probe", "controller", "standby_mode", "disk_color_mode", "alert_level"}

func jsonType(v any) string {
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

func collectStatusShape(t *testing.T) statusShape {
	t.Helper()
	keys := map[string]map[string]bool{}
	enums := map[string]map[string]bool{}
	add := func(m map[string]map[string]bool, k, v string) {
		if m[k] == nil {
			m[k] = map[string]bool{}
		}
		m[k][v] = true
	}
	products := []string{"DXP6800 Pro", "DX4600 Pro", "DXP8800 Plus", "DXP4800 Plus GT", "DXP4800 Plus", "DX4700+", "DXP2800", "iDX6011 Pro", "Unknown NAS"}
	configs := []string{
		"",
		"mode=\"status\"\nnetdev_status=\"link\"\nshutdown_effect=\"breath\"\nstandby_mode=\"off\"\ndisk_color_mode=\"temp\"\nalert_level=\"alert\"\n",
		"mode=\"manual\"\nnetdev_status=\"manual\"\nstandby_mode=\"normal\"\ndisk_color_mode=\"own\"\nalert_level=\"warning\"\n",
	}
	for _, product := range products {
		for i, cfg := range configs {
			for _, chipDown := range []bool{false, true} {
				r := newRig(t, product)
				writeSystem(r)
				r.config(cfg)
				r.chip.fail = chipDown
				var out bytes.Buffer
				r.c.Out = &out
				if rc := r.c.status(); rc != 0 {
					t.Fatalf("%s config %d: status rc %d", product, i, rc)
				}
				var got map[string]any
				if err := json.Unmarshal(out.Bytes(), &got); err != nil {
					t.Fatalf("%s: status is not JSON: %v\n%s", product, err, out.String())
				}
				for k, v := range got {
					add(keys, k, jsonType(v))
				}
				for _, k := range enumKeys {
					if s, ok := got[k].(string); ok {
						add(enums, k, s)
					}
				}
			}
		}
	}
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
	return statusShape{Keys: flat(keys), Enums: flat(enums)}
}

func TestStatusShapeMatchesRecord(t *testing.T) {
	got, err := json.MarshalIndent(collectStatusShape(t), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	if os.Getenv("UNC_UPDATE_SHAPE") == "1" {
		if err := os.WriteFile(filepath.FromSlash(shapeFile), got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(filepath.FromSlash(shapeFile))
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
		t.Errorf("the status answer changed; run UNC_UPDATE_SHAPE=1 go test ./internal/led/ledctl -run StatusShape and fix web/models/fixture.ts to match\n%s", strings.TrimSpace(fmt.Sprint(string(got))))
	}
}
