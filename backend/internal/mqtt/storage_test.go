package mqtt

import (
	"encoding/json"
	"math"
	"path/filepath"
	"strings"
	"testing"
)

const disksSample = `["parity"]
name="parity"
type="Parity"
["disk1"]
name="disk1"
type="Data"
fsType="xfs"
fsSize="1000"
fsFree="400"
fsUsed="600"
["disk2"]
name="disk2"
type="Data"
fsType="xfs"
fsSize="3000"
fsFree="2500"
fsUsed="500"
["boot"]
name="boot"
type="Cache"
fsType="auto"
fsSize="0"
fsFree="0"
fsUsed="0"
["fast"]
name="fast"
type="Cache"
fsType="btrfs"
fsSize="2000"
fsFree="1500"
fsUsed="500"
["nvme2"]
name="nvme2"
type="Cache"
["flash"]
name="flash"
type="Boot"
fsType="zfs"
fsSize="400"
fsFree="300"
fsUsed="100"
`

func TestReadStorage(t *testing.T) {
	p := filepath.Join(t.TempDir(), "disks.ini")
	writeFile(t, p, disksSample)
	s := readStorage(p)
	if s == nil || s.Array == nil || s.Array.Size != 4000*1024 || s.Array.Used != 1100*1024 || s.Array.Free != 2900*1024 {
		t.Fatalf("array: %+v", s)
	}
	if len(s.Array.Disks) != 2 || s.Array.Disks[0].Name != "disk1" || s.Array.Disks[0].Used != 600*1024 || s.Array.Disks[1].Name != "disk2" || s.Array.Disks[1].Size != 3000*1024 {
		t.Fatalf("each data disk by itself, in order: %+v", s.Array.Disks)
	}
	if b, _ := json.Marshal(s.Array); !strings.Contains(string(b), `"size_bytes":4096000`) || !strings.Contains(string(b), `"disks":[{"name":"disk1"`) {
		t.Fatalf("array JSON: %s", b)
	}
	if len(s.Pools) != 1 || s.Pools[0].Name != "fast" || s.Pools[0].FS != "btrfs" || s.Pools[0].Size != 2000*1024 {
		t.Fatalf("pools must hold the real pool only, not boot, flash or a pool without a file system: %+v", s.Pools)
	}
	if readStorage(filepath.Join(t.TempDir(), "none")) != nil {
		t.Fatal("no disks.ini: nothing to show")
	}
	writeFile(t, p, "[\"parity\"]\ntype=\"Parity\"\n")
	if readStorage(p) != nil {
		t.Fatal("parity alone has no space to show")
	}
}

func TestReadParity(t *testing.T) {
	p := filepath.Join(t.TempDir(), "var.ini")
	writeFile(t, p, "mdState=\"STARTED\"\nmdResync=\"0\"\nmdResyncPos=\"0\"\nmdResyncSize=\"1000\"\nmdResyncAction=\"check P\"\n")
	if readParity(p) != nil {
		t.Fatal("mdResync 0 means nothing runs")
	}
	writeFile(t, p, "mdState=\"STARTED\"\nmdResync=\"1000\"\nmdResyncPos=\"250\"\nmdResyncSize=\"1000\"\nmdResyncAction=\"check P\"\nmdResyncDb=\"100\"\nmdResyncDt=\"10\"\n")
	r := readParity(p)
	if r == nil || r.Action != "check" || math.Abs(r.Percent-25) > 0.001 || r.Speed != 10*1024 || r.ETA != 750*1024/(10*1024) {
		t.Fatalf("running check: %+v", r)
	}
	writeFile(t, p, "mdState=\"STARTED\"\nmdResync=\"1000\"\nmdResyncPos=\"500\"\nmdResyncSize=\"1000\"\nmdResyncAction=\"recon P\"\nmdResyncDb=\"0\"\nmdResyncDt=\"0\"\n")
	r = readParity(p)
	if r == nil || r.Action != "recon" || r.Speed != 0 || r.ETA != 0 {
		t.Fatalf("rebuild without a speed: %+v", r)
	}
	writeFile(t, p, "mdState=\"STOPPED\"\nmdResync=\"1000\"\nmdResyncPos=\"5\"\nmdResyncSize=\"1000\"\nmdResyncAction=\"check P\"\n")
	if readParity(p) != nil {
		t.Fatal("a stopped array runs nothing")
	}
}
