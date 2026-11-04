package chip

import (
	"reflect"
	"testing"
)

func TestDWCommands(t *testing.T) {
	// SMBus read word: command byte, restart, two reads, stop on the last.
	got := dwCommands([]dwMsg{{buf: []byte{0x5a}}, {read: true, buf: make([]byte, 2)}})
	want := []uint32{0x5a, 0x500, 0x300}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("read word: got %x, want %x", got, want)
	}

	// Block write: one message, stop on the last byte only.
	got = dwCommands([]dwMsg{{buf: []byte{0x80, 0x02, 0x11, 0x22}}})
	want = []uint32{0x80, 0x02, 0x11, 0x222}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("block write: got %x, want %x", got, want)
	}
}
