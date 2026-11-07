package nasctl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"
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

// feedbackSummary is the one-paragraph digest: MISMATCH, ok and not compared check ids, the fan chip, the LED probe
// and the I2C controllers without a driver. Every check of the evidence flows in on its own.
func feedbackSummary(checks []FBCheck, r Report, i2c I2CDiag) string {
	var bad, good, open []string
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
	var p []string
	if len(bad) > 0 {
		p = append(p, "MISMATCH: "+strings.Join(bad, ", ")+".")
	}
	if len(good) > 0 {
		p = append(p, "ok: "+strings.Join(good, ", ")+".")
	}
	if len(open) > 0 {
		p = append(p, "not compared: "+strings.Join(open, ", ")+".")
	}
	if r.Fan.Found {
		p = append(p, "Fan chip "+r.Fan.Chip+".")
	} else {
		p = append(p, "Fan chip not found.")
	}
	led := "LED probe " + r.LED.Probe
	if r.LED.Address != "" {
		led += fmt.Sprintf(" at %s, %d LEDs", r.LED.Address, r.LED.LEDCount)
	}
	p = append(p, led+".")
	var none []string
	for _, x := range i2c.Controllers {
		if x.Driver == "none" {
			none = append(none, x.BDF)
		}
	}
	if len(none) > 0 {
		p = append(p, "I2C controllers without driver: "+strings.Join(none, ", ")+".")
	}
	return cut(strings.Join(p, " "), summaryMax)
}

// BuildFeedback assembles the JSON result; diagMarkdown is the scrubbed text `diag` prints.
func BuildFeedback(a Anon, ev Evidence, r Report, i2c I2CDiag, kernel, diagMarkdown string, took time.Duration) Feedback {
	checks := feedbackChecks(ev)
	return Feedback{
		Schema:       "feedback/1",
		Model:        FBModel{ID: ev.Model.ID, Name: a.Meta(ev.Model.Name), Known: ev.Model.Known},
		Versions:     FBVersions{Plugin: ev.PluginVersion, Unraid: ev.UnraidVersion, Kernel: kernel},
		Summary:      a.Meta(feedbackSummary(checks, r, i2c)),
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
