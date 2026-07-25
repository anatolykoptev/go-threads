package threads

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

// captureWarns installs a test slog handler that records every record emitted
// through slog.SetDefault for the duration of the test, and restores the
// previous default afterwards so test order stays independent. The returned
// slice is appended to in place; callers inspect it after running the code
// under test.
func captureWarns(t *testing.T) *[]string {
	t.Helper()
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	var got []string
	h := &captureHandler{records: &got}
	slog.SetDefault(slog.New(h))
	return &got
}

type captureHandler struct {
	records *[]string
	attrs   []slog.Attr
}

func (h *captureHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }

func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	var sb strings.Builder
	sb.WriteString(r.Level.String())
	sb.WriteByte(' ')
	sb.WriteString(r.Message)
	r.Attrs(func(a slog.Attr) bool {
		sb.WriteByte(' ')
		sb.WriteString(a.Key)
		sb.WriteByte('=')
		sb.WriteString(a.Value.String())
		return true
	})
	*h.records = append(*h.records, sb.String())
	return nil
}

func (h *captureHandler) WithAttrs(attrs []slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(name string) slog.Handler       { return h }

// TestFlexBoolUnrecognisedValueWarns: an unrecognised value must decode to
// false, return no error, AND emit exactly one WARN containing the raw value
// — so a silent degrade (the v0.7.0 is_dash_eligible incident class) is
// visible in the journal instead of vanishing. Tolerate-and-continue stays;
// only the silence goes.
func TestFlexBoolUnrecognisedValueWarns(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"string yes", `"yes"`},
		{"numeric 2", `2`},
		{"object", `{}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			warns := captureWarns(t)
			var f flexBool
			if err := json.Unmarshal([]byte(tc.raw), &f); err != nil {
				t.Fatalf("Unmarshal(%s): unexpected error: %v", tc.raw, err)
			}
			if bool(f) != false {
				t.Errorf("Unmarshal(%s): flexBool = %v, want false (tolerate-and-default)", tc.raw, bool(f))
			}
			if len(*warns) != 1 {
				t.Fatalf("Unmarshal(%s): got %d WARN lines, want exactly 1: %v", tc.raw, len(*warns), *warns)
			}
			if !strings.Contains((*warns)[0], "WARN") {
				t.Errorf("Unmarshal(%s): warn line %q is not level WARN", tc.raw, (*warns)[0])
			}
			if !strings.Contains((*warns)[0], tc.raw) {
				t.Errorf("Unmarshal(%s): warn line %q does not contain the raw value", tc.raw, (*warns)[0])
			}
		})
	}
}

// TestFlexBoolRecognisedValuesNoLog: every recognised value decodes as
// before AND emits NO log line — the warn path is reserved for the
// unrecognised branch only, so recognised shapes do not spam the journal.
func TestFlexBoolRecognisedValuesNoLog(t *testing.T) {
	cases := []struct {
		name   string
		raw    string
		wantOK bool
	}{
		{"bool true", `true`, true},
		{"bool false", `false`, false},
		{"numeric 0", `0`, false},
		{"numeric 1", `1`, true},
		{"string 0", `"0"`, false},
		{"string 1", `"1"`, true},
		{"null", `null`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			warns := captureWarns(t)
			var f flexBool
			if err := json.Unmarshal([]byte(tc.raw), &f); err != nil {
				t.Fatalf("Unmarshal(%s): unexpected error: %v", tc.raw, err)
			}
			if bool(f) != tc.wantOK {
				t.Errorf("Unmarshal(%s): flexBool = %v, want %v", tc.raw, bool(f), tc.wantOK)
			}
			if len(*warns) != 0 {
				t.Errorf("Unmarshal(%s): got %d log lines, want 0: %v", tc.raw, len(*warns), *warns)
			}
		})
	}
}

// TestFlexBoolUnrecognisedValueTruncated: a hostile/huge payload must not
// flood the log — the raw value carried in the WARN is defensively
// truncated.
func TestFlexBoolUnrecognisedValueTruncated(t *testing.T) {
	warns := captureWarns(t)
	huge := `"` + strings.Repeat("x", 5000) + `"`
	var f flexBool
	if err := json.Unmarshal([]byte(huge), &f); err != nil {
		t.Fatalf("Unmarshal(huge): unexpected error: %v", err)
	}
	if bool(f) != false {
		t.Errorf("Unmarshal(huge): flexBool = %v, want false", bool(f))
	}
	if len(*warns) != 1 {
		t.Fatalf("Unmarshal(huge): got %d WARN lines, want 1: %v", len(*warns), *warns)
	}
	// The recorded raw value attribute must be bounded — far smaller than the
	// 5000-byte input. 512 is the cap; allow some slop for the surrounding
	// log line text.
	if len((*warns)[0]) > 1024 {
		t.Errorf("Unmarshal(huge): warn line length = %d, want <= 1024 (truncated): %q", len((*warns)[0]), (*warns)[0][:200])
	}
}
