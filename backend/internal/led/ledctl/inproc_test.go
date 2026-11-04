package ledctl

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

type recordingChip struct{ calls [][]string }

func (r *recordingChip) Exec(args []string, _, _ io.Writer) int {
	r.calls = append(r.calls, args)
	return 7
}

func TestInProcChipUsageAndValidation(t *testing.T) {
	c := InProcChip{}
	var out, errw bytes.Buffer

	if rc := c.Exec(nil, &out, &errw); rc != 2 || !strings.Contains(errw.String(), "Usage:") {
		t.Fatalf("no args: rc=%d stderr=%q", rc, errw.String())
	}

	errw.Reset()
	if rc := c.Exec([]string{"get", "nosuchled"}, &out, &errw); rc != 1 || !strings.HasPrefix(errw.String(), "ERROR:") {
		t.Fatalf("unknown led: rc=%d stderr=%q", rc, errw.String())
	}

	errw.Reset()
	if rc := c.Exec([]string{"set", "power", "color", "1", "2"}, nil, nil); rc != 1 {
		t.Fatalf("bad command with nil writers: rc=%d", rc)
	}
	if out.Len() != 0 {
		t.Fatalf("stdout must stay empty, got %q", out.String())
	}
}

func TestInProcChipEffectsGoToTheFallback(t *testing.T) {
	rec := &recordingChip{}
	c := InProcChip{Fallback: rec}
	if rc := c.Exec([]string{"--speed", "3", "effect", "rainbow", "all"}, nil, nil); rc != 7 {
		t.Fatalf("rc=%d, want the fallback's 7", rc)
	}
	if len(rec.calls) != 1 || rec.calls[0][2] != "effect" {
		t.Fatalf("fallback calls: %v", rec.calls)
	}

	var errw bytes.Buffer
	if rc := (InProcChip{}).Exec([]string{"effect", "rainbow", "all"}, nil, &errw); rc != 1 || !strings.Contains(errw.String(), "ERROR:") {
		t.Fatalf("without fallback: rc=%d stderr=%q", rc, errw.String())
	}
}

var _ Chip = InProcChip{}

func TestInProcChipPanicBecomesError(t *testing.T) {
	c := InProcChip{run: func([]string, io.Writer, io.Writer) int { panic("boom") }}
	var errw bytes.Buffer
	if rc := c.Exec([]string{"get", "power"}, nil, &errw); rc != 1 || !strings.Contains(errw.String(), "ERROR: LED controller: boom") {
		t.Fatalf("rc=%d stderr=%q", rc, errw.String())
	}
}
