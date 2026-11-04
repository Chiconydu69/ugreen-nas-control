package fan

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

const dmidecodeSample = `Handle 0x0010, DMI type 16, 23 bytes
Physical Memory Array
	Location: System Board Or Motherboard
	Number Of Devices: 2

Handle 0x0011, DMI type 17, 92 bytes
Memory Device
	Array Handle: 0x0010
	Size: 32 GiB
	Locator: Controller0-ChannelA-DIMM0
	Type: DDR5
	Speed: 5600 MT/s
	Manufacturer: Micron Technology
	Part Number: CT32G56C46S5.M16D1  
	Configured Memory Speed: 4800 MT/s

Handle 0x0012, DMI type 17, 92 bytes
Memory Device
	Size: No Module Installed
	Locator: Controller1-ChannelA-DIMM0
	Type: Unknown
	Speed: Unknown
`

func TestParseDmidecode(t *testing.T) {
	dimms := parseDmidecode(dmidecodeSample)
	if len(dimms) != 2 {
		t.Fatalf("%d modules", len(dimms))
	}
	d := dimms[0]
	if d.Locator != "Controller0-ChannelA-DIMM0" || d.SizeMB != 32768 || d.Type != "DDR5" ||
		d.SpeedMT != 5600 || d.ConfiguredMT != 4800 || d.PartNumber != "CT32G56C46S5.M16D1" {
		t.Errorf("%+v", d)
	}
	if dimms[1].SizeMB != 0 || dimms[1].SpeedMT != 0 {
		t.Errorf("empty slot: %+v", dimms[1])
	}
}

// fakeSys builds the parts of /sys that ReadHardware looks at, after a DXP6800 Pro.
func fakeSys(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	write := func(path, content string) {
		full := filepath.Join(dir, path)
		os.MkdirAll(filepath.Dir(full), 0o755)
		os.WriteFile(full, []byte(content), 0o644)
	}
	link := func(target, path string) {
		full := filepath.Join(dir, path)
		os.MkdirAll(filepath.Dir(full), 0o755)
		os.Symlink(filepath.Join(dir, target), full)
	}

	nvmes := []struct{ id, root, dev, model string }{
		{"nvme0", "0000:00:06.2", "0000:02:00.0", "CT500P310SSD8"},
		{"nvme1", "0000:00:1c.2", "0000:58:00.0", "WD Red SN700 1000GB"},
		{"nvme2", "0000:00:1c.0", "0000:57:00.0", "WD Red SN700 1000GB"},
		{"nvme3", "0000:00:06.0", "0000:01:00.0", "Samsung SSD 990 PRO 2TB"},
	}
	for _, n := range nvmes {
		base := "devices/pci0000:00/" + n.root + "/" + n.dev
		os.MkdirAll(filepath.Join(dir, base, "nvme", n.id), 0o755)
		write(base+"/nvme/"+n.id+"/model", n.model+"   \n")
		link(base+"/nvme/"+n.id, "class/nvme/"+n.id)
		link(base, "class/nvme/"+n.id+"/device")
		write("block/"+n.id+"n1/size", "3907029168\n")
	}

	// sda/sdb on the PCH controller (ata1, ata2), sdc..sdf on the ASMedia (ata3..ata6).
	disks := []struct {
		dev, pci string
		port     int
	}{
		{"sda", "0000:00:17.0", 1}, {"sdb", "0000:00:17.0", 2},
		{"sdc", "0000:5b:00.0", 3}, {"sdd", "0000:5b:00.0", 4},
		{"sde", "0000:5b:00.0", 5}, {"sdf", "0000:5b:00.0", 6},
	}
	for _, d := range disks {
		target := "devices/pci0000:00/" + d.pci + "/ata" + string(rune('0'+d.port)) + "/host0/target0:0:0/0:0:0:0"
		os.MkdirAll(filepath.Join(dir, target), 0o755)
		write(target+"/model", "ST8000VN004-2M21\n")
		link(target, "block/"+d.dev+"/device")
		write("block/"+d.dev+"/size", "15628053168\n")
		write("block/"+d.dev+"/queue/rotational", "1\n")
	}
	// A USB stick has no ata port and is no bay.
	os.MkdirAll(filepath.Join(dir, "devices/usb1/1-1"), 0o755)
	link("devices/usb1/1-1", "block/sdg/device")

	// Physical NICs have a device; a bridge does not.
	for _, n := range []struct{ name, speed, carrier string }{{"eth0", "10000", "1"}, {"eth1", "-1", "0"}, {"br0", "10000", "1"}} {
		if n.name != "br0" {
			os.MkdirAll(filepath.Join(dir, "class/net", n.name, "device"), 0o755)
		}
		write("class/net/"+n.name+"/speed", n.speed+"\n")
		write("class/net/"+n.name+"/carrier", n.carrier+"\n")
	}

	write("class/hwmon/hwmon0/name", "nvme\n")
	write("class/hwmon/hwmon0/temp1_input", "41850\n")
	os.MkdirAll(filepath.Join(dir, "devices/x/nvme3"), 0o755)
	link("devices/x/nvme3", "class/hwmon/hwmon0/device")
	// the Samsung (nvme3) is Unraid's cache, /boot is on the system slot SSD (nvme1)
	write("disks.ini", "[\"disk1\"]\nname=\"disk1\"\ndevice=\"sdc\"\nid=\"ST8000VN004-2M2101_WSD0AAAA\"\ntemp=\"36\"\nspundown=\"0\"\nrotational=\"1\"\n"+
		"[\"parity\"]\nname=\"parity\"\ndevice=\"sde\"\ntemp=\"*\"\nspundown=\"1\"\nrotational=\"1\"\n"+
		"[\"cache\"]\nname=\"cache\"\ndevice=\"nvme3n1\"\n")
	write("mounts", "/dev/nvme1n1p1 /boot vfat rw 0 0\n/dev/md1 /mnt/disk1 xfs rw 0 0\n")
	write("dmi/product_name", "DXP6800 Pro\n")
	write("dmidecode.sh", "#!/bin/sh\ncat <<'EOT'\n"+dmidecodeSample+"EOT\n")
	os.Chmod(filepath.Join(dir, "dmidecode.sh"), 0o755)

	oldSys, oldHwmon, oldINI, oldDMI, oldBin, oldMounts, oldEth, oldCache := sysDir, hwmonDir, disksINI, dmiDir, dmidecodeBn, mountsFile, ethtoolBn, cacheDir
	t.Cleanup(func() {
		sysDir, hwmonDir, disksINI, dmiDir, dmidecodeBn, mountsFile, ethtoolBn, cacheDir = oldSys, oldHwmon, oldINI, oldDMI, oldBin, oldMounts, oldEth, oldCache
	})
	cacheDir = ""
	sysDir, hwmonDir, disksINI = dir, filepath.Join(dir, "class/hwmon"), filepath.Join(dir, "disks.ini")
	dmiDir, dmidecodeBn, mountsFile = filepath.Join(dir, "dmi"), filepath.Join(dir, "dmidecode.sh"), filepath.Join(dir, "mounts")
}

func TestReadHardware(t *testing.T) {
	fakeSys(t)
	hw := ReadHardware()

	if len(hw.Memory) != 2 || hw.Memory[0].SizeMB != 32768 {
		t.Errorf("memory %+v", hw.Memory)
	}

	slots := map[string]string{}
	for _, n := range hw.NVMe {
		slots[n.ID] = n.Slot
	}
	want := map[string]string{"nvme0": "pcie", "nvme1": "system", "nvme2": "m2-2", "nvme3": "m2-1"}
	for id, slot := range want {
		if slots[id] != slot {
			t.Errorf("%s in %q, want %q", id, slots[id], slot)
		}
	}
	roles := map[string]string{}
	boots := map[string]bool{}
	for _, n := range hw.NVMe {
		roles[n.ID], boots[n.ID] = n.Role, n.Boot
	}
	if roles["nvme3"] != "cache" || !boots["nvme1"] || boots["nvme3"] || boots["nvme0"] || roles["nvme0"] != "" {
		t.Errorf("roles %v boot %v", roles, boots)
	}
	if hw.NVMe[0].Model != "CT500P310SSD8" || hw.NVMe[0].SizeBytes != 3907029168*512 {
		t.Errorf("nvme0 %+v", hw.NVMe[0])
	}
	if hw.NVMe[3].Temp == nil || *hw.NVMe[3].Temp != 42 || hw.NVMe[0].Temp != nil {
		t.Errorf("nvme temperatures %+v", hw.NVMe)
	}

	// bay 1..6 = ata3, ata4, ata5, ata6, ata1, ata2
	if len(hw.Bays) != 6 {
		t.Fatalf("%d bays", len(hw.Bays))
	}
	for i, dev := range []string{"sdc", "sdd", "sde", "sdf", "sda", "sdb"} {
		if !hw.Bays[i].Present || hw.Bays[i].Device != dev {
			t.Errorf("bay %d: %+v, want %s", i+1, hw.Bays[i], dev)
		}
	}
	if b := hw.Bays[0]; b.ID != "ST8000VN004-2M2101_WSD0AAAA" || b.Role != "disk1" || b.Temp == nil || *b.Temp != 36 || b.Model != "ST8000VN004-2M21" {
		t.Errorf("bay 1 %+v", b)
	}
	if b := hw.Bays[2]; b.Role != "parity" || !b.Standby || b.Temp != nil {
		t.Errorf("bay 3 %+v", b)
	}

	if len(hw.Net) != 2 || !hw.Net[0].Link || hw.Net[0].SpeedMbit != 10000 || hw.Net[1].Link {
		t.Errorf("net %+v", hw.Net)
	}
}

func TestReadHardwareOtherModelHasNoBays(t *testing.T) {
	fakeSys(t)
	os.WriteFile(filepath.Join(dmiDir, "product_name"), []byte("DX4600 Pro\n"), 0o644)
	if hw := ReadHardware(); hw.Bays != nil {
		t.Errorf("bays on another model: %+v", hw.Bays)
	}
}

func TestHardwareCache(t *testing.T) {
	fakeSys(t)
	cacheDir = t.TempDir()
	eth := filepath.Join(t.TempDir(), "ethtool.sh")
	os.WriteFile(eth, []byte("#!/bin/sh\necho '	Supported link modes:   100baseT/Full'\necho '	                        10000baseT/Full'\necho '	Supported pause frame use: Symmetric'\n"), 0o755)
	ethtoolBn = eth
	first := ReadHardware()
	if len(first.Memory) == 0 {
		t.Fatalf("no memory in the fake: %+v", first)
	}
	// The tools are gone, the second read still has what the first one learned.
	dmidecodeBn = "/nonexistent/dmidecode"
	ethtoolBn = "/nonexistent/ethtool"
	second := ReadHardware()
	if len(second.Memory) != len(first.Memory) {
		t.Errorf("memory not cached: %+v", second.Memory)
	}
	if len(first.Net) == 0 || first.Net[0].MaxMbit != 10000 {
		t.Errorf("fake ethtool not used: %+v", first.Net)
	}
	for i := range second.Net {
		if second.Net[i].MaxMbit != first.Net[i].MaxMbit {
			t.Errorf("max speed not cached: %+v vs %+v", second.Net, first.Net)
		}
	}
}

func TestReadHardwareWithoutDmidecode(t *testing.T) {
	fakeSys(t)
	dmidecodeBn = "/nonexistent/dmidecode"
	if hw := ReadHardware(); len(hw.Memory) != 0 || len(hw.NVMe) != 4 {
		t.Errorf("%+v", hw)
	}
}

func TestLayoutFor(t *testing.T) {
	if l := layoutFor("DXP6800 Pro"); len(l.bayPorts) != 6 || l.slots["0000:00:06.0"] != "m2-1" || l.slots["0000:00:1c.2"] != "system" || l.slots["0000:00:06.2"] != "pcie" {
		t.Errorf("DXP6800 Pro: %+v", l)
	}
	l := layoutFor("DXP4800 GT")
	if len(l.bayPorts) != 4 || l.bayPorts[3] != 4 || l.slots["0000:00:01.4"] != "m2-1" || l.slots["0000:00:01.6"] != "m2-2" {
		t.Errorf("DXP4800 GT: %+v", l)
	}
	for _, model := range []string{"DXP4800 Plus", "DXP4800", "DXP4800 Pro"} {
		if l := layoutFor(model); len(l.bayPorts) != 4 || l.bayPorts[0] != 1 || l.otherSlot != "m2" || l.slots != nil {
			t.Errorf("%q: %+v", model, l)
		}
	}
	if l := layoutFor("DXP8800 Plus"); len(l.bayPorts) != 8 || l.bayPorts[7] != 8 || l.otherSlot != "m2" {
		t.Errorf("DXP8800 Plus: %+v", l)
	}
	for _, model := range []string{"DX4600 Pro", ""} {
		if l := layoutFor(model); l.bayPorts != nil || l.slots != nil {
			t.Errorf("%q: %+v", model, l)
		}
	}
}

func TestReadHardwareDXP4800GT(t *testing.T) {
	fakeSys(t)
	os.WriteFile(filepath.Join(dmiDir, "product_name"), []byte("DXP4800 GT\n"), 0o644)
	hw := ReadHardware()
	if hw.Model != "DXP4800 GT" || len(hw.Bays) != 4 {
		t.Fatalf("%q, %d bays", hw.Model, len(hw.Bays))
	}
	// ata1..ata4 in order: sda, sdb, sdc, sdd.
	for i, dev := range []string{"sda", "sdb", "sdc", "sdd"} {
		if !hw.Bays[i].Present || hw.Bays[i].Device != dev {
			t.Errorf("bay %d: %+v, want %s", i+1, hw.Bays[i], dev)
		}
	}
}

func TestReadHardwareDXP4800Plus(t *testing.T) {
	fakeSys(t)
	os.WriteFile(filepath.Join(dmiDir, "product_name"), []byte("DXP4800 Plus\n"), 0o644)
	hw := ReadHardware()
	if len(hw.Bays) != 4 {
		t.Errorf("bays: %+v", hw.Bays)
	}
	for _, d := range hw.NVMe {
		if d.Slot != "m2" {
			t.Errorf("%s in slot %q", d.ID, d.Slot)
		}
	}
}

func TestParseMaxLinkSpeed(t *testing.T) {
	out := "Settings for eth1:\n\tSupported ports: [ TP ]\n\tSupported link modes:   100baseT/Full\n\t                        1000baseT/Full\n\t                        2500baseT/Full\n\t                        10000baseT/Full\n\tSupported pause frame use: Symmetric\n\tAdvertised link modes:  100baseT/Full\n"
	if n := parseMaxLinkSpeed(out); n != 10000 {
		t.Errorf("max %d", n)
	}
	if n := parseMaxLinkSpeed("Settings for eth0:\n"); n != 0 {
		t.Errorf("max without modes %d", n)
	}
}

func TestLayoutDXP2800AndIDX(t *testing.T) {
	if l := layoutFor("DXP2800"); len(l.bayPorts) != 2 || l.otherSlot != "m2" {
		t.Errorf("DXP2800: %+v", l)
	}
	// Without the sysfs tree of a real iDX6011 the bays are ata1 .. ata6.
	fakeSys(t)
	if p := idxBayPorts("iDX6011"); len(p) != 6 || p[0] != 1 || p[5] != 6 {
		t.Errorf("idx ports %v", p)
	}
	os.WriteFile(filepath.Join(dmiDir, "product_name"), []byte("iDX6011 Pro\n"), 0o644)
	if hw := ReadHardware(); len(hw.Bays) != 6 {
		t.Errorf("iDX bays: %+v", hw.Bays)
	}
}

// The fake tree has the ASMedia controller on ata3..6 and the Intel one on ata1..2: the plain iDX lists ASMedia
// first, the Pro puts the Intel ports on bays 3 and 4.
func TestIdxBayOrder(t *testing.T) {
	fakeSys(t)
	os.MkdirAll(filepath.Join(sysDir, "class", "ata_port"), 0o755)
	for port := 1; port <= 6; port++ {
		pci, vendor := "0000:00:17.0", "0x8086"
		if port > 2 {
			pci, vendor = "0000:5b:00.0", "0x1b21"
		}
		name := "ata" + string(rune('0'+port))
		os.Symlink(filepath.Join(sysDir, "devices", "pci0000:00", pci, name), filepath.Join(sysDir, "class", "ata_port", name))
		os.MkdirAll(filepath.Join(sysDir, "bus", "pci", "devices", pci), 0o755)
		os.WriteFile(filepath.Join(sysDir, "bus", "pci", "devices", pci, "vendor"), []byte(vendor+"\n"), 0o644)
	}
	for model, want := range map[string][]int{
		"iDX6011":     {3, 4, 5, 6, 1, 2},
		"iDX6012":     {3, 4, 5, 6, 1, 2},
		"iDX6011 Pro": {3, 4, 1, 2, 5, 6},
	} {
		if got := idxBayPorts(model); !slices.Equal(got, want) {
			t.Errorf("%s: bay ports %v, want %v", model, got, want)
		}
	}
	os.WriteFile(filepath.Join(dmiDir, "product_name"), []byte("iDX6011 Pro\n"), 0o644)
	hw := ReadHardware()
	if len(hw.Bays) != 6 {
		t.Fatalf("iDX6011 Pro bays: %+v", hw.Bays)
	}
}

func TestReadNetIO(t *testing.T) {
	dir := t.TempDir()
	old := sysDir
	t.Cleanup(func() { sysDir = old })
	sysDir = dir

	for _, n := range []struct{ name, rx, tx string }{{"eth0", "1000\n", "2000\n"}, {"eth1", "x\n", "5\n"}, {"eth2", "7\n", "8\n"}, {"br0", "1\n", "1\n"}} {
		nic := filepath.Join(dir, "class/net", n.name)
		os.MkdirAll(filepath.Join(nic, "statistics"), 0o755)
		if n.name != "br0" {
			os.MkdirAll(filepath.Join(nic, "device"), 0o755)
		}
		os.WriteFile(filepath.Join(nic, "statistics/rx_bytes"), []byte(n.rx), 0o644)
		os.WriteFile(filepath.Join(nic, "statistics/tx_bytes"), []byte(n.tx), 0o644)
	}

	got := ReadNetIO()
	if len(got) != 2 || got[0] != (NetIO{"eth0", 1000, 2000}) || got[1] != (NetIO{"eth2", 7, 8}) {
		t.Errorf("net_io %+v", got)
	}
}

func TestMaxLinkSpeedMissIsNotPermanent(t *testing.T) {
	oldEth, oldCache, oldTTL := ethtoolBn, cacheDir, missTTL
	t.Cleanup(func() { ethtoolBn, cacheDir, missTTL = oldEth, oldCache, oldTTL })
	cacheDir = t.TempDir()
	dir := t.TempDir()
	// No binary: nothing is cached.
	ethtoolBn = filepath.Join(dir, "missing")
	if n := maxLinkSpeed("eth9"); n != 0 {
		t.Fatalf("got %d", n)
	}
	if _, err := os.Stat(cachePath("maxspeed_miss")); err == nil {
		t.Error("a missing ethtool was cached as a miss")
	}
	// ethtool fails: a miss, believed for the TTL only.
	bad := filepath.Join(dir, "bad.sh")
	os.WriteFile(bad, []byte("#!/bin/sh\nexit 1\n"), 0o755)
	good := filepath.Join(dir, "good.sh")
	os.WriteFile(good, []byte("#!/bin/sh\necho '	Supported link modes:   2500baseT/Full'\necho '	Supported pause frame use: Symmetric'\n"), 0o755)
	ethtoolBn = bad
	maxLinkSpeed("eth9")
	ethtoolBn = good
	if n := maxLinkSpeed("eth9"); n != 0 {
		t.Errorf("miss not remembered within the TTL: %d", n)
	}
	missTTL = 0
	if n := maxLinkSpeed("eth9"); n != 2500 {
		t.Errorf("miss never expires: %d", n)
	}
}
