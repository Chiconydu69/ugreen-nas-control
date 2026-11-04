package fan

import "fmt"

// The IT8613E has five tachometer inputs and PWM outputs. Which output drives which fan differs per board, and
// only the DXP6800 Pro is known (it8613Channels). On the other IT8613E models the probe (probe.go) finds out.
//
// Registers: headers 2 to 4 are the ones already verified on the DXP6800 Pro (it8613Channels). Header 1 follows the
// same pattern of the ITE hardware-monitor family (tach 0x0d/0x18, duty 0x63, control 0x15). Header 5 has a tach
// input (0x82/0x4c) but no PWM output is written: its registers are not confirmed, so the probe reads that tach only.
// Header 1 and the tach of 5 are not confirmed on a device with this plugin. UGREEN's drivers have no header 1 and
// declare PWM 5 (control 0x1f, duty 0xa3) without ever using it (ug_it86x-cpufan.c:85-86@c75c5ab). Header 4's control
// register is unconfirmed: ours 0x7f, UGREEN's 0x1e (ug_it86x-cpufan.c:82-83@c75c5ab).
type iteHeader struct{ tachLow, tachHigh, duty, ctrl byte }

var it8613Headers = [...]iteHeader{
	{0x0d, 0x18, 0x63, 0x15},
	{0x0e, 0x19, 0x6b, 0x16},
	{0x0f, 0x1a, 0x73, 0x17},
	{0x80, 0x81, 0x7b, 0x7f},
	{0x82, 0x4c, 0, 0},
}

const itePWMCount = 4

func pwmID(n int) string { return fmt.Sprintf("pwm%d", n) }

func tachID(n int) string { return fmt.Sprintf("fan%d", n) }

func iteHeaderOf(prefix, id string, count int) (int, bool) {
	var n int
	if _, err := fmt.Sscanf(id, prefix+"%d", &n); err != nil || id != fmt.Sprintf("%s%d", prefix, n) || n < 1 || n > count {
		return 0, false
	}
	return n - 1, true
}

func iteChannel(id string, tach, pwm int) Channel {
	t, p := it8613Headers[tach], it8613Headers[pwm]
	return Channel{ID: id, TachLow: t.tachLow, TachHigh: t.tachHigh, Duty: p.duty, Ctrl: p.ctrl}
}

// iteCandidates is every output the probe may drive, each with its own header's tach (ids pwm1..pwm4). Also what
// restore falls back to on a model without a confirmed map.
func iteCandidates() []Channel {
	out := make([]Channel, itePWMCount)
	for i := range out {
		out[i] = iteChannel(pwmID(i+1), i, i)
	}
	return out
}

// iteRig is the probe's view of an IT8613E: outputs and tachs addressed by header, with the model floor enforced
// by the chip itself.
type iteRig struct{ chip *Chip }

func (r iteRig) candidates() *Chip {
	c := *r.chip
	c.Channels = iteCandidates()
	return &c
}

func (r iteRig) PWMs() []string {
	ids := make([]string, itePWMCount)
	for i := range ids {
		ids[i] = pwmID(i + 1)
	}
	return ids
}

func (r iteRig) Tachs() []string {
	ids := make([]string, len(it8613Headers))
	for i := range ids {
		ids[i] = tachID(i + 1)
	}
	return ids
}

func (r iteRig) SetPct(pwm string, pct int) error {
	i, ok := iteHeaderOf("pwm", pwm, itePWMCount)
	if !ok {
		return fmt.Errorf("unknown output %q", pwm)
	}
	return r.candidates().SetPWM(iteChannel(pwm, i, i), percentToDuty(pct, 255))
}

func (r iteRig) RPM(tach string) (int, error) {
	i, ok := iteHeaderOf("fan", tach, len(it8613Headers))
	if !ok {
		return 0, fmt.Errorf("unknown tach %q", tach)
	}
	return r.chip.RPM(iteChannel(tach, i, 0))
}

func (r iteRig) Snapshot() (Snapshot, error) { return r.candidates().Snapshot() }

func (r iteRig) Restore(s Snapshot) error { return r.candidates().Restore(s) }
