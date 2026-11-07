package parity

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// LEDCTL_PARITY_SHARD=k/n runs every n-th scenario (index mod n == k) of TestParity and TestNasCtl, so CI can split the run over n jobs.
// Unset or empty: all scenarios.
var shardIdx, shardN int

func parseShard(s string) (idx, n int, err error) {
	if s == "" {
		return 0, 0, nil
	}
	a, b, ok := strings.Cut(s, "/")
	if ok {
		idx, err = strconv.Atoi(a)
	}
	if ok && err == nil {
		n, err = strconv.Atoi(b)
	}
	if !ok || err != nil || n < 1 || idx < 0 || idx >= n {
		return 0, 0, fmt.Errorf("LEDCTL_PARITY_SHARD=%q: want k/n with 0 <= k < n", s)
	}
	return idx, n, nil
}

func inShard(i int) bool { return shardN == 0 || i%shardN == shardIdx }

// Every golden belongs to exactly one scenario, and the shards of any size together select every scenario exactly once.
func TestShardsCoverAllScenarios(t *testing.T) {
	var names []string
	for _, s := range scenarios() {
		names = append(names, s.name)
	}
	nParity := len(names)
	for _, s := range nasScenarios() {
		names = append(names, "nasctl-"+s.name)
	}
	files, err := filepath.Glob(filepath.Join("testdata", "golden", "*"))
	if err != nil {
		t.Fatal(err)
	}
	var onDisk []string
	for _, f := range files {
		onDisk = append(onDisk, strings.TrimSuffix(filepath.Base(f), filepath.Ext(f)))
	}
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	sort.Strings(onDisk)
	if strings.Join(sorted, "\n") != strings.Join(onDisk, "\n") {
		t.Fatalf("scenarios and goldens differ: %d scenarios, %d goldens", len(sorted), len(onDisk))
	}
	for n := 1; n <= 4; n++ {
		seen := map[string]int{}
		for k := 0; k < n; k++ {
			saved := [2]int{shardIdx, shardN}
			shardIdx, shardN = k, n
			for i := range nParity {
				if inShard(i) {
					seen[names[i]]++
				}
			}
			for i := range len(names) - nParity {
				if inShard(i) {
					seen[names[nParity+i]]++
				}
			}
			shardIdx, shardN = saved[0], saved[1]
		}
		if len(seen) != len(names) {
			t.Fatalf("%d shards select %d of %d scenarios", n, len(seen), len(names))
		}
		for name, c := range seen {
			if c != 1 {
				t.Fatalf("%d shards: %s runs %d times", n, name, c)
			}
		}
	}
}
