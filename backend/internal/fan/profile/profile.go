// Package profile is the fan profile: a small JSON file with the fan settings of one NAS (schema 1) and the importers
// that turn the settings of other fan tools into one. Everything is a pure function of the bytes it is given: nothing
// here writes a file, runs a command or touches the fan chip; the importers only read what the caller loaded.
//
// The settings go through Rules (implemented by package fan: the same ranges LoadConfig uses) before they leave this
// package, so a profile can never carry a value the daemon would reject.
package profile

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	Schema = 1
	Kind   = "ugreen-nas-fan-profile"
	// MaxBytes is the size limit of everything that is read: an uploaded file, a rival file, a profile.
	MaxBytes = 256 << 10
	maxKeys  = 200
)

// Sources of a Result.
const (
	SourceOwn          = "own"
	SourceFanCtrlPlus  = "fanctrlplus"
	SourceFanCtrlPlus2 = "fanctrlplus2"
	SourceDynamix      = "dynamix"
	SourceITKuny       = "itkuny"
)

// Profile is the file format.
type Profile struct {
	Schema  int               `json:"schema"`
	Kind    string            `json:"kind"`
	Model   string            `json:"model,omitempty"`
	Created string            `json:"created,omitempty"`
	Fan     map[string]string `json:"fan"`
	Notes   string            `json:"notes,omitempty"`
}

// Skipped is a setting of the source that was not taken over.
type Skipped struct {
	Key    string `json:"key"`
	Reason string `json:"reason"`
	File   string `json:"file,omitempty"`
}

// Result is what a parser gives: the mapped profile and everything the user should know before applying it.
type Result struct {
	Source      string            `json:"source"`
	Title       string            `json:"title"`
	Profile     Profile           `json:"profile"`
	Warnings    []string          `json:"warnings"`
	NotImported []Skipped         `json:"not_imported"`
	Provenance  map[string]string `json:"provenance"`
}

// Rules is what the fan package knows about its settings.
type Rules interface {
	// Normalize checks one key and clamps its value into the range the daemon accepts. note says what was changed
	// ("" when nothing). An error means the key is not a profile setting or the value cannot be repaired.
	Normalize(key, value string) (clean, note string, err error)
}

// Env is what the importers ask of the NAS they run on; nil functions resolve nothing.
type Env struct {
	// ResolveDisk maps a /dev/disk/by-id name to the Unraid name of the drive (disk1, cache, nvme0).
	ResolveDisk func(byID string) (string, bool)
	// ResolveDevice maps a device name (sdb, nvme0n1) to the Unraid name.
	ResolveDevice func(device string) (string, bool)
}

func (e Env) disk(id string) (string, bool) {
	if e.ResolveDisk == nil {
		return "", false
	}
	return e.ResolveDisk(id)
}

func (e Env) device(dev string) (string, bool) {
	if e.ResolveDevice == nil {
		return "", false
	}
	return e.ResolveDevice(dev)
}

func newResult(source string) *Result {
	return &Result{
		Source:      source,
		Profile:     Profile{Schema: Schema, Kind: Kind, Fan: map[string]string{}},
		Warnings:    []string{},
		NotImported: []Skipped{},
		Provenance:  map[string]string{},
	}
}

func (r *Result) warnf(format string, args ...any) {
	r.Warnings = append(r.Warnings, fmt.Sprintf(format, args...))
}

func (r *Result) skip(file, key, reason string) {
	r.NotImported = append(r.NotImported, Skipped{Key: key, Reason: reason, File: file})
}

// set records a setting and where it came from.
func (r *Result) set(key, value, from string) {
	r.Profile.Fan[key] = value
	r.Provenance[key] = from
}

// CheckText refuses what is not a small text file: too long, NUL or other control bytes, invalid UTF-8.
func CheckText(b []byte) error {
	if len(b) > MaxBytes {
		return fmt.Errorf("the file is larger than %d KB", MaxBytes>>10)
	}
	if !utf8.Valid(b) {
		return fmt.Errorf("the file is not text (not valid UTF-8)")
	}
	for _, c := range b {
		if c < 0x20 && c != '\n' && c != '\r' && c != '\t' {
			return fmt.Errorf("the file is not text (control characters)")
		}
	}
	return nil
}

// Encode is the profile file: indented JSON, keys sorted.
func (p Profile) Encode() ([]byte, error) {
	p.Schema, p.Kind = Schema, Kind
	if p.Fan == nil {
		p.Fan = map[string]string{}
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// Token identifies the settings of a profile (not its date or notes): the preview hands it out, apply checks that
// the profile it is about to write still has it.
func (p Profile) Token() string {
	keys := make([]string, 0, len(p.Fan))
	for k := range p.Fan {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, k := range keys {
		_, _ = fmt.Fprintf(h, "%d:%s=%d:%s\n", len(k), k, len(p.Fan[k]), p.Fan[k])
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Decode reads a profile file. Unknown top-level fields are ignored; a newer schema, another kind, a file that is not
// JSON, or values of the wrong type are refused or dropped (see Warnings).
func Decode(b []byte) (*Result, error) {
	if err := CheckText(b); err != nil {
		return nil, err
	}
	var raw struct {
		Schema  int            `json:"schema"`
		Kind    string         `json:"kind"`
		Model   string         `json:"model"`
		Created string         `json:"created"`
		Fan     map[string]any `json:"fan"`
		Notes   string         `json:"notes"`
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("not a profile file: %v", err)
	}
	if raw.Kind != Kind {
		return nil, fmt.Errorf("not a profile of this plugin")
	}
	if raw.Schema > Schema {
		return nil, fmt.Errorf("the profile has schema %d, newer than this plugin understands (%d); update the plugin first", raw.Schema, Schema)
	}
	if raw.Schema < 1 {
		return nil, fmt.Errorf("the profile has no valid schema")
	}
	if len(raw.Fan) > maxKeys {
		return nil, fmt.Errorf("the profile has more than %d settings", maxKeys)
	}
	r := newResult(SourceOwn)
	r.Profile.Model, r.Profile.Created, r.Profile.Notes = clip(raw.Model, 80), clip(raw.Created, 40), clip(raw.Notes, 2000)
	r.Title = "UGREEN NAS Control profile"
	keys := make([]string, 0, len(raw.Fan))
	for k := range raw.Fan {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		s, ok := raw.Fan[k].(string)
		if !ok {
			r.skip("", k, "the value is not text")
			continue
		}
		r.set(k, s, "profile file")
	}
	return r, nil
}

func clip(s string, n int) string {
	s = strings.Map(func(c rune) rune {
		if c < 0x20 {
			return ' '
		}
		return c
	}, s)
	if len(s) > n {
		s = s[:n]
		for !utf8.ValidString(s) {
			s = s[:len(s)-1]
		}
	}
	return s
}

// Finish runs every setting through the rules: values are clamped (with a warning), settings the rules do not know
// or cannot repair are dropped (listed as not imported). It is the last step of every parser.
func (r *Result) Finish(rules Rules) {
	keys := make([]string, 0, len(r.Profile.Fan))
	for k := range r.Profile.Fan {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := r.Profile.Fan[k]
		clean, note, err := rules.Normalize(k, v)
		switch {
		case err != nil:
			delete(r.Profile.Fan, k)
			delete(r.Provenance, k)
			r.skip("", k, err.Error())
		case note != "":
			r.warnf("%s: %s", k, note)
			r.Profile.Fan[k] = clean
		default:
			r.Profile.Fan[k] = clean
		}
	}
	sort.SliceStable(r.NotImported, func(i, j int) bool {
		if r.NotImported[i].File != r.NotImported[j].File {
			return r.NotImported[i].File < r.NotImported[j].File
		}
		return r.NotImported[i].Key < r.NotImported[j].Key
	})
}
