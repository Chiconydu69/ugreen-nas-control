package chip

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHSV(t *testing.T) {
	cases := map[float64]rgb{
		0:   {255, 0, 0},
		60:  {255, 255, 0},
		120: {0, 255, 0},
		240: {0, 0, 255},
		360: {255, 0, 0},
		-60: {255, 0, 255},
	}
	for hue, want := range cases {
		if got := hsv(hue); got != want {
			t.Errorf("hsv(%v) = %v, want %v", hue, got, want)
		}
	}
}

func TestRainbowFrame(t *testing.T) {
	got := rainbowFrame(0, 3, false)
	want := []rgb{{255, 0, 0}, {0, 255, 0}, {0, 0, 255}}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("frame = %v, want %v", got, want)
		}
	}

	rev := rainbowFrame(0, 3, true)
	if rev[1] != (rgb{0, 0, 255}) || rev[2] != (rgb{0, 255, 0}) {
		t.Fatalf("reverse frame = %v", rev)
	}

	if got := rainbowFrame(6, 1, false)[0]; got != hsv(60) {
		t.Fatalf("step 6 = %v, want %v", got, hsv(60))
	}
}

func TestTemperatureColor(t *testing.T) {
	cases := map[float64]rgb{
		20: {0, 120, 255},
		35: {0, 120, 255},
		40: {0, 160, 169},
		45: {0, 200, 83},
		55: {255, 200, 0},
		65: {255, 40, 40},
		90: {255, 40, 40},
	}
	for celsius, want := range cases {
		if got := temperatureColor(celsius); got != want {
			t.Errorf("temperatureColor(%v) = %v, want %v", celsius, got, want)
		}
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCPUTemperature(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "hwmon0", "name"), "nvme\n")
	writeFile(t, filepath.Join(root, "hwmon0", "temp1_input"), "80000\n")
	writeFile(t, filepath.Join(root, "hwmon3", "name"), "coretemp\n")
	writeFile(t, filepath.Join(root, "hwmon3", "temp1_input"), "47000\n")
	writeFile(t, filepath.Join(root, "hwmon3", "temp2_input"), "52500\n")

	// The NVMe sensor is ignored; the hottest CPU sensor wins.
	got, err := cpuTemperature(root)
	if err != nil || got != 52.5 {
		t.Fatalf("cpuTemperature = %v, %v; want 52.5", got, err)
	}

	if _, err := cpuTemperature(t.TempDir()); err == nil {
		t.Fatal("expected an error without a CPU sensor")
	}
}

func TestDiskActivity(t *testing.T) {
	root := t.TempDir()
	stat := filepath.Join(root, "sda", "stat")
	writeFile(t, stat, "100 0 0 0 50 0 0 0 0 0 0\n")

	before := diskActivity(root)
	if diskActivity(root) != before {
		t.Fatal("fingerprint changed without activity")
	}

	writeFile(t, stat, "101 0 0 0 50 0 0 0 0 0 0\n")
	if diskActivity(root) == before {
		t.Fatal("fingerprint unchanged after a read")
	}
}

func TestValidateEffect(t *testing.T) {
	if err := validateEffect(effectOptions{name: "rainbow", speed: 4}); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []effectOptions{{name: "disco"}, {name: "rainbow", speed: 5}, {name: "temperature", speed: -1}} {
		if err := validateEffect(bad); err == nil {
			t.Errorf("%+v: expected an error", bad)
		}
	}
}
