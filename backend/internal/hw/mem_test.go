package hw

import "testing"

func TestCheckMemAccess(t *testing.T) {
	for _, tc := range []struct {
		addr  uint64
		width int
		ok    bool
	}{
		{0xFE001020, 4, true}, {0xFED8035B, 1, true}, {0xFED8035B, 4, false},
		{0xFE001021, 4, false}, {0xFE001020, 2, false}, {0xFE001020, 8, false}, {0xFE001020, 0, false},
	} {
		if err := CheckMemAccess(tc.addr, tc.width); (err == nil) != tc.ok {
			t.Errorf("0x%X/%d: %v", tc.addr, tc.width, err)
		}
	}
}
