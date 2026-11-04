package chip

import (
	"bytes"
	"reflect"
	"testing"
)

func TestCommandPacketMatchesKernelDriver(t *testing.T) {
	// led-ugreen.c: cksum = 0xa1 + command + param1..param4, buf = {id, a0 01 00 00, cmd, p1..p4, hi, lo}
	got := commandPacket(4, cmdColor, [4]byte{255, 128, 0, 0})
	sum := 0xa1 + cmdColor + 255 + 128
	want := []byte{4, 0xa0, 0x01, 0x00, 0x00, cmdColor, 255, 128, 0, 0, byte(sum >> 8), byte(sum)}

	if !bytes.Equal(got, want) {
		t.Fatalf("packet = % x, want % x", got, want)
	}
}

func TestGTCommandPacketMatchesKernelDriver(t *testing.T) {
	// led-ugreen.c (chip 0xc5b2): no ID, cksum = 0xa1 + 0x02 + 255 + 128 = 0x0222, low byte first.
	got := gtCommandPacket(cmdColor, [4]byte{255, 128, 0, 0})
	want := []byte{0xa0, 0x01, 0x00, 0x00, 0x02, 0xff, 0x80, 0x00, 0x00, 0x22, 0x02}

	if !bytes.Equal(got, want) {
		t.Fatalf("packet = % x, want % x", got, want)
	}
}

func TestPeriodParams(t *testing.T) {
	// cycle 2100 = 0x0834, on 1200 = 0x04b0
	if got, want := periodParams(cmdBreath, 1200, 900), [4]byte{0x08, 0x34, 0x04, 0xb0}; got != want {
		t.Fatalf("periodParams = % x, want % x", got, want)
	}

	// Blink clamped to 100..0x7fff like the kernel driver.
	const cycle = 100 + 0x7fff
	if got, want := periodParams(cmdBlink, 10, 99999), [4]byte{cycle >> 8, cycle & 0xff, 0, 100}; got != want {
		t.Fatalf("clamped periodParams = % x, want % x", got, want)
	}
}

// UGREEN raises breath to on >= 1000 ms and off >= 500 ms before writing (leds-mcu-28a48.c:485-500@c75c5ab).
func TestBreathMinimums(t *testing.T) {
	if got, want := periodParams(cmdBreath, 10, 10), [4]byte{0x05, 0xdc, 0x03, 0xe8}; got != want {
		t.Fatalf("breath 10/10 = % x, want % x (cycle 1500, on 1000)", got, want)
	}
	if got, want := periodParams(cmdBreath, 1000, 500), periodParams(cmdBreath, 1, 1); got != want {
		t.Fatalf("breath at the minimum = % x, want % x", got, want)
	}
	if got, want := periodParams(cmdBlink, 10, 10), [4]byte{0x00, 0xc8, 0x00, 0x64}; got != want {
		t.Fatalf("blink 10/10 = % x, want % x (cycle 200, on 100)", got, want)
	}
}

func statusBlock(mode, brightness, r, g, b byte, cycle, on int) []byte {
	block := []byte{mode, brightness, r, g, b, byte(cycle >> 8), byte(cycle), byte(on >> 8), byte(on)}
	sum := 0
	for _, v := range block {
		sum += int(v)
	}
	return append(block, byte(sum>>8), byte(sum))
}

func TestParseStatus(t *testing.T) {
	got, err := parseStatus("disk2", statusBlock(3, 43, 255, 38, 0, 2100, 1200))
	if err != nil {
		t.Fatal(err)
	}

	want := ledStatus{Name: "disk2", Mode: "breath", Brightness: 43, Color: "255 38 0", OnMs: 1200, OffMs: 900}
	if got != want {
		t.Fatalf("status = %+v, want %+v", got, want)
	}
}

func TestPlainLine(t *testing.T) {
	s := ledStatus{Name: "disk2", Mode: "breath", Brightness: 43, Color: "255 38 0", OnMs: 1200, OffMs: 900}
	if got, want := plainLine(s), "disk2 breath 43 255 38 0 1200 900"; got != want {
		t.Fatalf("plainLine = %q, want %q", got, want)
	}
}

func TestParseStatusRejectsBadBlocks(t *testing.T) {
	good := statusBlock(1, 255, 1, 2, 3, 0, 0)

	broken := append([]byte(nil), good...)
	broken[10] ^= 0xff

	badMode := statusBlock(7, 255, 1, 2, 3, 0, 0)

	for name, block := range map[string][]byte{
		"short":    good[:10],
		"checksum": broken,
		"all zero": make([]byte, statusLength),
		"bad mode": badMode,
	} {
		if _, err := parseStatus("power", block); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestParseLeds(t *testing.T) {
	got, err := parseLeds("power,disk3")
	if err != nil || !reflect.DeepEqual(got, []string{"power", "disk3"}) {
		t.Fatalf("parseLeds = %v, %v", got, err)
	}

	if all, _ := parseLeds("all"); len(all) != 10 {
		t.Fatalf("all = %v", all)
	}

	if _, err := parseLeds("disk9"); err == nil {
		t.Fatal("disk9 should be rejected")
	}
}

func TestBuildCommand(t *testing.T) {
	cases := []struct {
		args    []string
		command byte
		params  [4]byte
	}{
		{[]string{"color", "0", "52", "243"}, cmdColor, [4]byte{0, 52, 243, 0}},
		{[]string{"brightness", "43"}, cmdBrightness, [4]byte{43}},
		{[]string{"on"}, cmdOnOff, [4]byte{1}},
		{[]string{"off"}, cmdOnOff, [4]byte{0}},
		{[]string{"blink", "450", "450"}, cmdBlink, periodParams(cmdBlink, 450, 450)},
		{[]string{"breath", "1200", "900"}, cmdBreath, periodParams(cmdBreath, 1200, 900)},
	}

	for _, c := range cases {
		command, params, err := buildCommand(c.args)
		if err != nil || command != c.command || params != c.params {
			t.Errorf("%v: got %x % x %v, want %x % x", c.args, command, params, err, c.command, c.params)
		}
	}

	for _, bad := range [][]string{{}, {"color", "1", "2"}, {"brightness", "256"}, {"blink", "-1", "2"}, {"dance"}} {
		if _, _, err := buildCommand(bad); err == nil {
			t.Errorf("%v: expected an error", bad)
		}
	}
}

func TestDXP480TWhite(t *testing.T) {
	// Same writes as `i2cset -y <bus> 0x26 <register> <value> b` in ich777's driver: set white, set orange, clear
	// orange, orange mode none, white mode none (0xa0 sets bits, 0xb1 clears them).
	want := [][2]byte{{0xa0, 1}, {0xa0, 2}, {0xb1, 2}, {0x51, 0}, {0x50, 0}}

	if dxp480tAddr != 0x26 || len(dxp480tWhite) != len(want) {
		t.Fatalf("got address 0x%x and %d writes", dxp480tAddr, len(dxp480tWhite))
	}
	for i := range want {
		if dxp480tWhite[i] != want[i] {
			t.Errorf("write %d: got %x, want %x", i, dxp480tWhite[i], want[i])
		}
	}
}

func TestIdxCommandPacket(t *testing.T) {
	// Frames of the iDX6011 plugins (omgapuppy), written as i2cset ... s: header, command,
	// four parameters, then the checksum high byte first.
	cases := []struct {
		command byte
		params  [4]byte
		want    []byte
	}{
		{0x04, [4]byte{}, []byte{0xa0, 0x01, 0x00, 0x00, 0x04, 0, 0, 0, 0, 0x00, 0xa5}},
		{0x01, [4]byte{0xff}, []byte{0xa0, 0x01, 0x00, 0x00, 0x01, 0xff, 0, 0, 0, 0x01, 0xa1}},
		{0x02, [4]byte{0xff, 0xff, 0xff}, []byte{0xa0, 0x01, 0x00, 0x00, 0x02, 0xff, 0xff, 0xff, 0, 0x03, 0xa0}},
		{0x03, [4]byte{0xff}, []byte{0xa0, 0x01, 0x00, 0x00, 0x03, 0xff, 0, 0, 0, 0x01, 0xa3}},
	}

	for _, c := range cases {
		if got := idxCommandPacket(c.command, c.params); !bytes.Equal(got, c.want) {
			t.Errorf("command %x: got % x, want % x", c.command, got, c.want)
		}
	}
}

func TestIdxProLedIDs(t *testing.T) {
	saved := getLayout()
	defer SetLayout(saved)

	want := map[string]byte{"power": 0, "netdev": 1, "disk1": 3, "disk6": 8}

	SetLayout("idx-pro")
	for name, id := range want {
		if got, err := ledID(name); err != nil || got != id {
			t.Errorf("idx-pro %s: got %d %v, want %d", name, got, err, id)
		}
	}

	SetLayout("idx")
	if got, _ := ledID("disk1"); got != 2 {
		t.Errorf("idx disk1: got %d, want 2", got)
	}
}

func TestLan2LED(t *testing.T) {
	saved := getLayout()
	defer SetLayout(saved)

	SetLayout("idx-pro")
	if id, err := ledID("netdev2"); err != nil || id != 2 {
		t.Errorf("idx-pro netdev2: got %d %v, want 2", id, err)
	}
	all, err := parseLeds("all")
	if err != nil || len(all) != 11 || all[2] != "netdev2" || all[3] != "disk1" {
		t.Errorf("idx-pro all = %v %v", all, err)
	}
	for i, name := range all { // every LED of the layout has its own register, in order
		if id, _ := ledID(name); int(id) != i {
			t.Errorf("idx-pro %s: register %d, want %d", name, id, i)
		}
	}

	for _, layout := range []string{"", "idx", "dxp2800"} {
		SetLayout(layout)
		if _, err := ledID("netdev2"); err == nil {
			t.Errorf("layout %q: netdev2 should be rejected", layout)
		}
		if all, _ := parseLeds("all"); len(all) != 10 {
			t.Errorf("layout %q: all = %v", layout, all)
		}
	}
}

func TestDXP2800Layout(t *testing.T) {
	for product, want := range map[string]string{
		"DXP2800": layoutDXP2800, "DXP6800": layoutDXP2800, "DXP6800S": layoutDXP2800, "DXP6800 Pro": "", "DXP6800 Plus": "", "DXP2800 GT": layoutCS201x, "DXP2800S": "", "DXP4800": "", "iDX6011": "idx", "": "",
	} {
		if got := detectLayout(product); got != want {
			t.Errorf("%q: got %q, want %q", product, got, want)
		}
	}

	saved := getLayout()
	defer SetLayout(saved)
	SetLayout(layoutDXP2800)
	for i, name := range []string{"power", "netdev", "disk1", "disk2"} { // same IDs as UGREEN's DXP2800 table
		if id, err := ledID(name); err != nil || int(id) != i {
			t.Errorf("%s: got %d %v, want %d", name, id, err, i)
		}
	}

	for i, name := range []string{"power", "netdev", "disk1", "disk2", "disk3", "disk4", "disk5", "disk6"} { // DXP6800 table, leds-mcu.c:110-115
		SetLayout(detectLayout("DXP6800"))
		if id, err := ledID(name); err != nil || int(id) != i {
			t.Errorf("DXP6800 %s: got %d %v, want %d", name, id, err, i)
		}
	}
}

func TestIdxLayoutByProduct(t *testing.T) {
	for product, want := range map[string]string{
		"iDX6011": "idx", "iDX6012": "idx", "iDX6011 Pro": "idx-pro", "iDX6012 Pro": "",
		"iDX6012B": "idx", "DXP4800 Pro": "", "iDX6013": "", "": "",
	} {
		if got := IdxLayout(product); got != want {
			t.Errorf("%q: got %q, want %q", product, got, want)
		}
	}
}

// reset is the iDX mode reset (command 0x04, zero parameters), frame checksum 0x00a5
// (noodlemctwoodle src/monitor.sh:122, omgapuppy src/monitor.sh:167).
func TestResetCommand(t *testing.T) {
	command, params, err := buildCommand([]string{"reset"})
	if err != nil || command != 0x04 || params != [4]byte{} {
		t.Fatalf("got %x %v %v", command, params, err)
	}
	want := []byte{0xa0, 0x01, 0x00, 0x00, 0x04, 0, 0, 0, 0, 0x00, 0xa5}
	if got := idxCommandPacket(command, params); !bytes.Equal(got, want) {
		t.Errorf("got % x", got)
	}
}
