package mqtt

import (
	"os"
	"strconv"
	"strings"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/fan"
)

var (
	varINI   = envOr("UGREEN_VAR_INI", "/var/local/emhttp/var.ini")
	disksINI = envOr("UGREEN_DISKS_INI", "/var/local/emhttp/disks.ini")
)

// PubSpace is the size of a file system. Unraid counts in blocks of 1024 bytes.
type PubSpace struct {
	Name string `json:"name,omitempty"`
	FS   string `json:"fs,omitempty"`
	Size int64  `json:"size_bytes"`
	Used int64  `json:"used_bytes"`
	Free int64  `json:"free_bytes"`
}

// PubStorage is the array (all data disks together) and the pools, without the boot stick and without pools that
// have no file system (a disk that is not formatted or not mounted).
type PubStorage struct {
	Array *PubArray  `json:"array"`
	Pools []PubSpace `json:"pools"`
}

// PubArray is the array as a whole and, in Disks, each data disk by itself (name disk1, disk2, ...).
type PubArray struct {
	PubSpace
	Disks []PubSpace `json:"disks"`
}

// PubParity is a running parity check, rebuild or clear. It is nil while nothing runs.
type PubParity struct {
	Action  string  `json:"action"` // check, recon, clear
	Percent float64 `json:"percent"`
	Speed   int64   `json:"speed_bps,omitempty"`
	ETA     int64   `json:"eta_s,omitempty"`
}

// readSections parses Unraid's disks.ini: ["name"] lines followed by key="value" lines.
func readSections(path string) []map[string]string {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []map[string]string
	var cur map[string]string
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			cur = map[string]string{}
			out = append(out, cur)
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok && cur != nil {
			cur[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"`)
		}
	}
	return out
}

func space(m map[string]string) (PubSpace, bool) {
	size, e1 := strconv.ParseInt(m["fsSize"], 10, 64)
	used, e2 := strconv.ParseInt(m["fsUsed"], 10, 64)
	free, e3 := strconv.ParseInt(m["fsFree"], 10, 64)
	if e1 != nil || e2 != nil || e3 != nil || size <= 0 {
		return PubSpace{}, false
	}
	return PubSpace{Name: m["name"], FS: m["fsType"], Size: size * 1024, Used: used * 1024, Free: free * 1024}, true
}

// readStorage reads disks.ini only, so no disk is woken up. It returns nil when there is nothing to show.
func readStorage(path string) *PubStorage {
	secs := readSections(path)
	if secs == nil {
		return nil
	}
	st := &PubStorage{Pools: []PubSpace{}}
	var arr PubArray
	for _, m := range secs {
		sp, ok := space(m)
		if !ok {
			continue
		}
		switch m["type"] {
		case "Data":
			arr.Size, arr.Used, arr.Free = arr.Size+sp.Size, arr.Used+sp.Used, arr.Free+sp.Free
			arr.Disks = append(arr.Disks, sp)
			st.Array = &arr
		case "Cache":
			st.Pools = append(st.Pools, sp)
		}
	}
	if st.Array == nil && len(st.Pools) == 0 {
		return nil
	}
	return st
}

// readParity reads var.ini. mdResync is 0 while nothing runs and the size of the job while one does;
// mdResyncPos is how far it is, mdResyncDb blocks were done in the last mdResyncDt seconds.
func readParity(path string) *PubParity {
	v := fan.ReadValues(path)
	size, _ := strconv.ParseInt(v["mdResyncSize"], 10, 64)
	pos, _ := strconv.ParseInt(v["mdResyncPos"], 10, 64)
	if n, _ := strconv.ParseInt(v["mdResync"], 10, 64); n == 0 || size <= 0 || v["mdState"] != "STARTED" {
		return nil
	}
	if pos < 0 {
		pos = 0
	}
	if pos > size {
		pos = size
	}
	p := &PubParity{Percent: float64(pos) * 100 / float64(size)}
	act := strings.Fields(v["mdResyncAction"])
	if len(act) > 0 {
		p.Action = act[0]
	}
	db, _ := strconv.ParseInt(v["mdResyncDb"], 10, 64)
	dt, _ := strconv.ParseInt(v["mdResyncDt"], 10, 64)
	if db > 0 && dt > 0 {
		p.Speed = db * 1024 / dt
		p.ETA = (size - pos) * 1024 / p.Speed
	}
	return p
}
