package nasctl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/led/chip"
)

// Feedback is the result of `diag --json` (schema feedback/1): what the plugin's Feedback page shows and sends.
type Feedback struct {
	Schema       string     `json:"schema"`
	Model        FBModel    `json:"model"`
	Versions     FBVersions `json:"versions"`
	Summary      string     `json:"summary"`
	Checks       []FBCheck  `json:"checks"`
	DiagMarkdown string     `json:"diag_markdown"`
	TookMS       int64      `json:"took_ms"`
}

type FBModel struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Known bool   `json:"known"`
}

type FBVersions struct {
	Plugin string `json:"plugin"`
	Unraid string `json:"unraid"`
	Kernel string `json:"kernel"`
}

// FBCheck is one evidence check; OK is null where nothing can be compared.
type FBCheck struct {
	Area     string `json:"area"`
	ID       string `json:"id"`
	Expected any    `json:"expected"`
	Detected any    `json:"detected"`
	OK       *bool  `json:"ok"`
}

// summaryMax bounds the summary in bytes: the page puts it into a link of limited length.
const summaryMax = 600

// feedbackChecks flattens the evidence areas in their fixed order.
func feedbackChecks(ev Evidence) []FBCheck {
	out := []FBCheck{}
	for _, a := range areaOrder {
		for _, c := range ev.Areas[a].Checks {
			out = append(out, FBCheck{Area: a, ID: c.ID, Expected: c.Expected, Detected: c.Detected, OK: c.OK})
		}
	}
	return out
}

// listClause is "label a, b, c." that fits room bytes: ids that do not fit are left out and the list ends in "…".
// It is empty when not even the first id fits.
func listClause(label string, ids []string, room int) string {
	const more = ", …"
	for n := len(ids); n > 0; n-- {
		s := label + strings.Join(ids[:n], ", ")
		if n < len(ids) {
			s += more
		}
		if len(s)+1 <= room {
			return s + "."
		}
	}
	return ""
}

// modelLEDCount is how many LEDs the model has: power and network plus one per bay, the second LAN LED of the
// iDX6011 Pro on top. The controller answers status for every slot, so the probe count says nothing about the model.
// ok is false for a model the registry does not know.
func modelLEDCount(reg *RegModel, product string) (n int, ok bool) {
	if reg == nil {
		return 0, false
	}
	switch reg.LED {
	case "none":
		return 0, true
	case "power":
		return 1, true
	}
	n = 2
	if reg.Bays != nil {
		n += *reg.Bays
	}
	if chip.IdxLayout(product) == "idx-pro" {
		n++
	}
	return n, true
}

// feedbackSummary is the one-paragraph digest: MISMATCH, ok and not compared check ids, the fan chip, the LED probe
// and the I2C controllers without a driver. It is at most summaryMax bytes by construction: the fan chip and LED
// clauses always stay, the id lists take what is left (MISMATCH first) and end in "…" where ids were left out.
// leds is the model's LED count, or -1 to show what the probe answered.
func feedbackSummary(checks []FBCheck, r Report, i2c I2CDiag, leds int) string {
	var bad, good, open, none []string
	for _, c := range checks {
		switch {
		case c.OK == nil:
			open = append(open, c.ID)
		case *c.OK:
			good = append(good, c.ID)
		default:
			bad = append(bad, c.ID)
		}
	}
	fan := "Fan chip not found."
	if r.Fan.Found {
		fan = "Fan chip " + r.Fan.Chip + "."
	}
	led := "LED probe " + r.LED.Probe
	if r.LED.Address != "" {
		if leds < 0 {
			leds = r.LED.LEDCount
		}
		led += fmt.Sprintf(" at %s, %d LEDs", r.LED.Address, leds)
	}
	led += "."
	for _, x := range i2c.Controllers {
		if x.Driver == "none" {
			none = append(none, x.BDF)
		}
	}
	room := summaryMax - len(fan) - len(led) - 2
	take := func(label string, ids []string) string {
		s := listClause(label, ids, room)
		if s != "" {
			room -= len(s) + 1
		}
		return s
	}
	sBad := take("MISMATCH: ", bad)
	sOpen := take("not compared: ", open)
	sNone := take("I2C controllers without driver: ", none)
	sGood := take("ok: ", good)
	var p []string
	for _, s := range []string{sBad, sGood, sOpen, fan, led, sNone} {
		if s != "" {
			p = append(p, s)
		}
	}
	return strings.Join(p, " ")
}

// BuildFeedback assembles the JSON result; diagMarkdown is the scrubbed text `diag` prints.
func BuildFeedback(a Anon, ev Evidence, reg *RegModel, r Report, i2c I2CDiag, kernel, diagMarkdown string, took time.Duration) Feedback {
	checks := feedbackChecks(ev)
	leds, known := modelLEDCount(reg, r.Machine.Product)
	if !known {
		leds = -1
	}
	return Feedback{
		Schema:       "feedback/1",
		Model:        FBModel{ID: ev.Model.ID, Name: a.Meta(ev.Model.Name), Known: ev.Model.Known},
		Versions:     FBVersions{Plugin: ev.PluginVersion, Unraid: ev.UnraidVersion, Kernel: kernel},
		Summary:      a.Meta(feedbackSummary(checks, r, i2c, leds)),
		Checks:       checks,
		DiagMarkdown: diagMarkdown,
		TookMS:       took.Milliseconds(),
	}
}

// JSON is one line, without HTML escaping (<host> stays readable).
func (f Feedback) JSON() []byte {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(f)
	return b.Bytes()
}
