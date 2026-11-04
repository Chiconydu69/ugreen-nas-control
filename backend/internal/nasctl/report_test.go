package nasctl

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/fan"
	"github.com/ugreen-nas-control/ugreen-nas-control/internal/fan/conflict"
)

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// secrets are everything that identifies the NAS or its owner; planted in every source the report reads.
var secrets = []string{
	"SN-PRODUCT-SECRET", "UUID-SECRET-1234", "SN-BOARD-SECRET", "SN-CHASSIS-SECRET", "ASSET-SECRET",
	"aa:bb:cc:dd:ee:ff", "AA-BB-CC-DD-EE-FF", "192.168.17.42", "10.20.30.40", "fe80::1234:5678:9abc:def0",
	"WD-WCC4SECRET", "WDC WD80EFAX-SECRET", "ST4000-SECRET", "SamsungSECRETSSD", "NVMESERIAL-SECRET",
	"nas-hostname-secret", "FANSERIAL-SECRET", "LEDSERIAL-SECRET", "cpuserial-secret",
	"sdb", "sdz", "parity", "cache", "disk1", "disk2", "0000:02:00.0", "nvme0", "nvme1",
}

func TestReportLeaksNothing(t *testing.T) {
	dir := t.TempDir()
	dmi := filepath.Join(dir, "dmi")
	for name, text := range map[string]string{
		"sys_vendor": "UGREEN", "product_name": "DXP4800 Plus", "product_family": "NAS", "board_vendor": "UGREEN", "board_name": "UGREEN-BOARD",
		"bios_vendor": "American Megatrends", "bios_version": "1.2 aa:bb:cc:dd:ee:ff 192.168.17.42", "bios_date": "03/01/2024",
		"product_serial": "SN-PRODUCT-SECRET", "product_uuid": "UUID-SECRET-1234", "board_serial": "SN-BOARD-SECRET",
		"chassis_serial": "SN-CHASSIS-SECRET", "chassis_asset_tag": "ASSET-SECRET", "board_asset_tag": "ASSET-SECRET",
	} {
		write(t, filepath.Join(dmi, name), text+"\n")
	}
	proc := filepath.Join(dir, "proc")
	write(t, filepath.Join(proc, "cpuinfo"), "processor\t: 0\nmodel name\t: Intel(R) N100\nserial\t\t: cpuserial-secret\nflags\t: nas-hostname-secret\n")
	write(t, filepath.Join(dir, "unraid-version"), "version=\"7.1.2\"\n")
	boot := filepath.Join(dir, "boot")
	write(t, filepath.Join(boot, "ugreen-nas-control.plg"), "<!ENTITY name \"ugreen-nas-control\">\n<!ENTITY version   \"3.6\">\n<!ENTITY host \"nas-hostname-secret\">\n")

	two := 40
	in := Inputs{
		Env: Env{DMIDir: dmi, Proc: proc, UnraidFile: filepath.Join(dir, "unraid-version"), PluginsBoot: boot},
		Hardware: fan.Hardware{
			Model:  "DXP4800 Plus",
			Memory: []fan.DIMM{{Locator: "DIMM0 SN-PRODUCT-SECRET", SizeMB: 16384, Type: "DDR5", SpeedMT: 4800, Manufacturer: "Micron", PartNumber: "WD-WCC4SECRET"}},
			NVMe:   []fan.NVMe{{ID: "nvme0", Model: "SamsungSECRETSSD NVMESERIAL-SECRET", Slot: "m2-1", PCI: "0000:02:00.0", Role: "cache", Temp: &two}, {ID: "nvme1", Slot: "m2-2", Role: "disk2"}},
			Bays: []fan.Bay{
				{Bay: 1, Present: true, Device: "sdb", Model: "WDC WD80EFAX-SECRET", HDD: true, Role: "parity", ID: "WDC_WD80EFAX_WD-WCC4SECRET", Temp: &two},
				{Bay: 2, Present: true, Device: "sdz", Model: "ST4000-SECRET", HDD: false, Role: "disk1", ID: "ST4000-SECRET_ZZZ"},
				{Bay: 3},
			},
			Net: []fan.NetPort{{Name: "eth0", Link: true, SpeedMbit: 1000, MaxMbit: 2500}},
		},
		BayPorts: []int{1, 2, 3, 4},
		FanProbe: []byte(`{"ok": true, "chip": "it8613", "revision": 5, "base": "0x0a00", "serial": "FANSERIAL-SECRET", "host": "nas-hostname-secret", "mac": "aa:bb:cc:dd:ee:ff"}`),
		LEDProbe: []byte(`{"bus": "/dev/i2c-4", "address": "0x3a", "leds": ["power", "netdev", "disk1", "disk2"], "serial": "LEDSERIAL-SECRET", "ip": "10.20.30.40"}`),
		LEDRan:   true, LEDOK: true,
		Rivals: []conflict.Conflict{{Kind: conflict.KindPlugin, ID: "fanctrlplus", Name: "FanCtrl Plus", Controls: []string{conflict.Fan}}},
	}

	data, err := json.MarshalIndent(Build(in), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, s := range secrets {
		if strings.Contains(strings.ToLower(text), strings.ToLower(s)) {
			t.Errorf("the report contains %q:\n%s", s, text)
		}
	}

	var got Report
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Machine.Product != "DXP4800 Plus" || got.CPU != "Intel(R) N100" || got.PluginVersion != "3.6" || got.UnraidVersion != "7.1.2" {
		t.Errorf("machine: %+v cpu %q plugin %q unraid %q", got.Machine, got.CPU, got.PluginVersion, got.UnraidVersion)
	}
	if !got.Fan.Found || got.Fan.Chip != "it8613" || got.Fan.Base != "0x0a00" {
		t.Errorf("fan: %+v", got.Fan)
	}
	if got.LED.Probe != "ok" || got.LED.Bus != "/dev/i2c-4" || got.LED.Address != "0x3a" || got.LED.LEDCount != 4 {
		t.Errorf("led: %+v", got.LED)
	}
	if len(got.Bays) != 3 || got.Bays[0].Port != 1 || got.Bays[0].Kind != "hdd" || got.Bays[1].Kind != "ssd" || got.Bays[2].Present {
		t.Errorf("bays: %+v", got.Bays)
	}
	if strings.Join(got.NVMe, ",") != "m2-1,m2-2" || len(got.Network) != 1 || got.Network[0].MaxMbit != 2500 || got.Rivals[0] != "fanctrlplus" {
		t.Errorf("nvme %v net %v rivals %v", got.NVMe, got.Network, got.Rivals)
	}
	if got.Machine.BIOSVersion != "1.2 <removed> <removed>" {
		t.Errorf("addresses in a DMI string were not removed: %q", got.Machine.BIOSVersion)
	}
}

func TestReportWithoutAnything(t *testing.T) {
	r := Build(Inputs{Env: Env{DMIDir: t.TempDir() + "/none"}})
	data, _ := json.Marshal(r)
	if !strings.Contains(string(data), `"bays":[]`) || !strings.Contains(string(data), `"probe":"not run"`) {
		t.Errorf("%s", data)
	}
}
