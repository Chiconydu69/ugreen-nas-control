package chip

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/hw"
	"github.com/ugreen-nas-control/ugreen-nas-control/internal/testenv"
)

// The LED controller of UGREEN NAS devices (I2C address 0x3a), as described by
// miskcoo/ugreen_leds_controller: one ID per LED, 12-byte block writes with a
// checksum, and an 11-byte status block per LED at register 0x81 + ID.
//
// Cross-checked against UGREEN's GPL kernel drivers (facts only, no code; paths below drivers/ugreen/ of
// ugreen-opensource kernel-6.12 @ c75c5ab): address 0x3a, chip id 0xC5B2 at register 0x5A (leds-mcu-28a48.c:17,
// 863-879), op codes 0x01..0x05 (28a48.c:38-42), ack register 0x80 (28a48.c:211-222), status at 0x81 + ID, 11 bytes,
// big-endian checksum over the first 9 (28a48.c:524-576).

const (
	controllerAddr = 0x3a

	cmdBrightness = 0x01
	cmdColor      = 0x02
	cmdOnOff      = 0x03
	cmdBlink      = 0x04
	cmdBreath     = 0x05

	regLastCommandOK = 0x80
	regStatusBase    = 0x81
	statusLength     = 11

	// Blink on and off each take at least 100 ms; breath on at least 1000 ms and off at least 500 ms, both enforced
	// by UGREEN's drivers before they write (28a48.c:485-500, leds-mcu.c:378-386@c75c5ab).
	minPeriod    = 100
	minBreathOn  = 1000
	minBreathOff = 500
	maxPeriod    = 0x7fff

	// layoutDXP2800 is the DXP2800, DXP6800 and DXP6800S (UGREEN serves the three with the same driver path): the legacy
	// frame first, the idx frame as a fallback (see framesFor).
	layoutDXP2800 = "dxp2800"
)

var ledNames = []string{"power", "netdev", "disk1", "disk2", "disk3", "disk4", "disk5", "disk6", "disk7", "disk8"}

// lan2Name is the second LAN LED of the iDX6011 Pro, register 2 ("network_stat2", leds-mcu.c:104-108@c75c5ab);
// it exists on the "idx-pro" layout only.
const lan2Name = "netdev2"

// allLeds is every LED of the layout in use, in register order.
func allLeds() []string {
	if getLayout() == layoutCS201x {
		return ledNames[:argbChannels]
	}
	if getLayout() != "idx-pro" {
		return ledNames
	}

	return append(slices.Clone(ledNames[:2]), append([]string{lan2Name}, ledNames[2:]...)...)
}

var modeNames = []string{"off", "on", "blink", "breath"}

// layout names a LED controller family that differs from the DX/DXP one: "idx-pro"
// (UGREEN iDX6011 Pro: two LAN LEDs, so disk1 is register 3) and "idx" (the same
// frames, disk1 is register 2). Both write SMBus blocks without the LED ID in the
// payload and with a big-endian checksum, as the vendor driver does (after klein0r's
// work on miskcoo/ugreen_leds_controller#93; leds-mcu.c:199-219@c75c5ab). "dxp2800" is
// the DXP2800: legacy frame with the idx frame as fallback, same LED IDs as the DX/DXP
// protocol. It comes from the model name, or from UGREEN_LAYOUT to try it by hand;
// empty is the DX/DXP protocol.
var layoutState struct {
	mu  sync.Mutex
	set bool
	v   string
}

// getLayout is the layout in use; the first call reads UGREEN_LAYOUT and the DMI model name (not at process start).
func getLayout() string {
	layoutState.mu.Lock()
	defer layoutState.mu.Unlock()

	if !layoutState.set {
		layoutState.v, layoutState.set = detectLayout(hw.ProductName(hw.DMIDir)), true
	}

	return layoutState.v
}

func SetLayout(v string) {
	layoutState.mu.Lock()
	layoutState.v, layoutState.set = v, true
	layoutState.mu.Unlock()
}

// UseProduct picks the layout from a model name given by the caller (UGREEN_PRODUCT, a test DMI file);
// UGREEN_LAYOUT still wins.
func UseProduct(product string) { SetLayout(detectLayout(product)) }

func detectLayout(product string) string {
	if v := testenv.Get("UGREEN_LAYOUT"); v != "" {
		return v
	}

	if v := IdxLayout(product); v != "" {
		return v
	}
	if isCS201xProduct(product) {
		return layoutCS201x
	}
	// Exact names, as UGREEN's driver compares them: DXP2800, DXP6800 and DXP6800S take the no-ID frame, 4 LEDs for the
	// DXP2800, 8 for the DXP6800 and DXP6800S (leds-mcu.c:62-63,171-173,558-561,627-629@c75c5ab; names :93-115). "DXP6800 Pro"
	// and "DXP6800 Plus" are not in UGREEN's list and stay on the DX/DXP frame.
	if product == "DXP2800" || product == "DXP6800" || product == "DXP6800S" {
		return layoutDXP2800
	}

	return ""
}

func idxFrames() bool {
	l := getLayout()
	return l == "idx" || l == "idx-pro"
}

func ledID(name string) (byte, error) {
	if name == lan2Name {
		if getLayout() != "idx-pro" {
			return 0, fmt.Errorf("LED %q exists on the iDX6011 Pro only", name)
		}
		return 2, nil
	}

	i := slices.Index(ledNames, name)
	if i < 0 || (getLayout() == layoutCS201x && i >= argbChannels) {
		return 0, fmt.Errorf("unknown LED %q (power, netdev, disk1..disk8)", name)
	}
	if getLayout() == "idx-pro" && i >= 2 {
		i++ // the second LAN LED sits in between
	}
	return byte(i), nil
}

// commandPacket is the block written to register <id>: the ID, a fixed header,
// the command with four parameters and a big-endian checksum. The checksum
// covers the packet with a zero in place of the ID (leds-mcu-28a48.c:199-211@c75c5ab).
func commandPacket(id, command byte, params [4]byte) []byte {
	packet := []byte{id, 0xa0, 0x01, 0x00, 0x00, command, params[0], params[1], params[2], params[3]}

	sum := 0
	for _, b := range packet[1:] {
		sum += int(b)
	}

	return append(packet, byte(sum>>8), byte(sum))
}

// gtCommandPacket is the GT variant: an SMBus block payload without the ID and
// with a little-endian checksum over the same bytes.
//
// Open question, behaviour deliberately unchanged: the maintainer verified this little-endian checksum on a
// DXP4800 GT, while UGREEN's driver sends the same no-ID frame for the GT with a BIG-endian checksum
// (leds-mcu.c:211-214@c75c5ab; the captured frame in the init comment at :595 is big-endian) and our own status
// read of the GT already needs big-endian. Either the MCU ignores the checksum of writes or one of the two orders is
// wrong. A GT test (write colour with each order, read the status, check register 0x80) would settle it.
func gtCommandPacket(command byte, params [4]byte) []byte {
	packet := []byte{0xa0, 0x01, 0x00, 0x00, command, params[0], params[1], params[2], params[3]}

	sum := 0
	for _, b := range packet {
		sum += int(b)
	}

	return append(packet, byte(sum), byte(sum>>8))
}

// idxCommandPacket is the iDX6011 variant: an SMBus block payload without the ID and
// with the checksum (0xa1 + command + parameters) high byte first, the shape UGREEN's driver sends to the
// models of leds-mcu.c (leds-mcu.c:199-219@c75c5ab); the DXP2800 fallback uses it too.
func idxCommandPacket(command byte, params [4]byte) []byte {
	packet := []byte{0xa0, 0x01, 0x00, 0x00, command, params[0], params[1], params[2], params[3]}

	sum := 0
	for _, b := range packet {
		sum += int(b)
	}

	return append(packet, byte(sum>>8), byte(sum))
}

// periodParams encodes blink/breath timing as the controller expects it: the whole cycle first, then the "on"
// part, each time clamped to what UGREEN's drivers accept (see minPeriod).
//
// Open question, unchanged: for blink UGREEN's old driver sends the on time as second field (28a48.c:501-503@c75c5ab)
// but its newer leds-mcu.c sends the OFF time (leds-mcu.c:387-395@c75c5ab, "timer2"). We send the on time for every
// frame, as the community drivers do; which one the iDX/DXP2800/GT firmware wants is unchecked. At the usual
// symmetric blink (on == off) both are the same.
func periodParams(command byte, onMs, offMs int) [4]byte {
	minOn, minOff := minPeriod, minPeriod
	if command == cmdBreath {
		minOn, minOff = minBreathOn, minBreathOff
	}

	on := min(max(onMs, minOn), maxPeriod)
	off := min(max(offMs, minOff), maxPeriod)
	cycle := on + off

	return [4]byte{byte(cycle >> 8), byte(cycle), byte(on >> 8), byte(on)}
}

type ledStatus struct {
	Name       string `json:"name"`
	Mode       string `json:"mode"`
	Brightness int    `json:"brightness"`
	Color      string `json:"color"`
	OnMs       int    `json:"on_ms"`
	OffMs      int    `json:"off_ms"`
}

var errBadStatus = errors.New("invalid status block")

// plainLine is the --plain output: "<name> <mode> <brightness> <R> <G> <B> <on_ms> <off_ms>".
func plainLine(s ledStatus) string {
	return fmt.Sprintf("%s %s %d %s %d %d", s.Name, s.Mode, s.Brightness, s.Color, s.OnMs, s.OffMs)
}

// parseStatus decodes the 11-byte status block: mode, brightness, R, G, B,
// cycle (2 bytes), on time (2 bytes), checksum (2 bytes) over the first nine.
func parseStatus(name string, block []byte) (ledStatus, error) {
	if len(block) != statusLength {
		return ledStatus{}, errBadStatus
	}

	sum := 0
	for _, b := range block[:9] {
		sum += int(b)
	}
	if sum == 0 || sum != int(block[9])<<8|int(block[10]) || int(block[0]) >= len(modeNames) {
		return ledStatus{}, errBadStatus
	}

	cycle := int(block[5])<<8 | int(block[6])
	on := int(block[7])<<8 | int(block[8])

	return ledStatus{
		Name:       name,
		Mode:       modeNames[block[0]],
		Brightness: int(block[1]),
		Color:      fmt.Sprintf("%d %d %d", block[2], block[3], block[4]),
		OnMs:       on,
		OffMs:      cycle - on,
	}, nil
}

func parseLeds(arg string) ([]string, error) {
	if arg == "all" {
		return allLeds(), nil
	}

	var names []string
	for _, name := range strings.Split(arg, ",") {
		if _, err := ledID(name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, nil
}

func parseByte(arg string) (byte, error) {
	n, err := strconv.Atoi(arg)
	if err != nil || n < 0 || n > 255 {
		return 0, fmt.Errorf("%q is not a value between 0 and 255", arg)
	}
	return byte(n), nil
}

func parseMs(arg string) (int, error) {
	n, err := strconv.Atoi(arg)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%q is not a duration in ms", arg)
	}
	return n, nil
}

// buildCommand turns "color 255 0 0", "brightness 128", "on", "off",
// "blink 500 500" or "breath 1200 900" into a controller command.
func buildCommand(args []string) (byte, [4]byte, error) {
	if slices.Equal(args, []string{"reset"}) {
		return buildReset()
	}
	return buildPlainCommand(args)
}

// buildReset is the mode reset the iDX front panel needs before a steady on: command 0x04 with zero timing.
func buildReset() (byte, [4]byte, error) { return cmdBlink, [4]byte{}, nil }

func buildPlainCommand(args []string) (byte, [4]byte, error) {
	var params [4]byte

	if len(args) == 0 {
		return 0, params, errors.New("missing command")
	}

	want := map[string]int{"color": 3, "brightness": 1, "on": 0, "off": 0, "blink": 2, "breath": 2}
	n, ok := want[args[0]]
	if !ok {
		return 0, params, fmt.Errorf("unknown command %q", args[0])
	}
	if len(args)-1 != n {
		return 0, params, fmt.Errorf("%s needs %d value(s)", args[0], n)
	}

	switch args[0] {
	case "color":
		for i := range 3 {
			b, err := parseByte(args[1+i])
			if err != nil {
				return 0, params, err
			}
			params[i] = b
		}
		return cmdColor, params, nil

	case "brightness":
		b, err := parseByte(args[1])
		if err != nil {
			return 0, params, err
		}
		params[0] = b
		return cmdBrightness, params, nil

	case "on", "off":
		if args[0] == "on" {
			params[0] = 1
		}
		return cmdOnOff, params, nil

	default:
		on, err := parseMs(args[1])
		if err != nil {
			return 0, params, err
		}
		off, err := parseMs(args[2])
		if err != nil {
			return 0, params, err
		}
		command := byte(cmdBlink)
		if args[0] == "breath" {
			command = cmdBreath
		}
		return command, periodParams(command, on, off), nil
	}
}

// DXP480T Plus: address of the power LED controller and the register writes (register, value) for a steady
// white LED. UGREEN's N76E003 driver lists 0x31 and 0x26 as candidate addresses
// (dxp480t/leds-mcu-n76e003.c:40-43@c75c5ab); 0x26 is the one reported on a device. Register 0xa0 SETS bits of the
// LED bit field (bit 0 white, bit 1 orange, shifted by the LED's position), 0xb1 CLEARS them, 0x50 and 0x51 are the
// mode registers of white and orange (0 = none; same file :21-23,30-33,236-241,257-270). So the five writes are:
// white on, orange on, orange off, orange mode none, white mode none. The end state (white on, orange off, both modes
// none) equals UGREEN's own init, which writes modes none, clears orange, sets white (:404-420); only a short orange
// flash in between is ours. Kept as tested by ich777 on a DXP480T Plus.
const (
	dxp480tAddr = 0x26

	regSetBits    = 0xa0
	regClearBits  = 0xb1
	regModeWhite  = 0x50
	regModeOrange = 0x51
	bitWhite      = 1
	bitOrange     = 2
	modeNone      = 0
)

var dxp480tWhite = [][2]byte{
	{regSetBits, bitWhite},
	{regSetBits, bitOrange},
	{regClearBits, bitOrange},
	{regModeOrange, modeNone},
	{regModeWhite, modeNone},
}

// IdxLayout is the layout of an iDX601x front panel by its DMI product name: "idx-pro" for exactly
// "iDX6011 Pro" (two LAN LEDs, disk registers shifted by one), "idx" for the plain iDX6011 and iDX6012
// (one LAN LED), "" for every other model, including any other name with "Pro" (an "iDX6012 Pro" does not exist).
//
// Facts, not code, from MIT-licensed community plugins (see docs/third-party.md):
//   - the plain iDX6012 is handled like the iDX6011: omgapuppy/unraid-ugreen-idx6011-led-fan
//     src/idx6011-fan-status.c:76 (model "iDX6011" or "iDX6012", commit 6196dc0), src/monitor.sh:118-122.
//   - the Pro is told apart by "Pro" in the product name: noodlemctwoodle/unraid-ugreen-idx6011-pro
//     (commit d202a7c) src/panel/fan.h:38.
func IdxLayout(product string) string {
	if !strings.HasPrefix(product, "iDX6011") && !strings.HasPrefix(product, "iDX6012") {
		return ""
	}
	if product == "iDX6011 Pro" {
		return "idx-pro"
	}
	if strings.Contains(product, "Pro") {
		return "" // no other Pro exists (UGREEN lists "iDX6011 Pro" only, leds-mcu.c:88-90@c75c5ab)
	}
	return "idx"
}
