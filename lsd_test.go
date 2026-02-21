package threads

import "testing"

func TestLSDRegex(t *testing.T) {
	tests := []struct {
		name  string
		html  string
		want  string
		found bool
	}{
		{
			name:  "standard format",
			html:  `some content LSD",[],{"token":"abc123def456"},1234] more content`,
			want:  "abc123def456",
			found: true,
		},
		{
			name:  "longer token",
			html:  `LSD",[],{"token":"AVqDh-2lkJ8"},99]`,
			want:  "AVqDh-2lkJ8",
			found: true,
		},
		{
			name:  "embedded in larger HTML",
			html:  `<html><script>require("LSD",[],{"token":"xyzToken789"},42])</script></html>`,
			want:  "xyzToken789",
			found: true,
		},
		{
			name:  "no match",
			html:  `<html><body>No LSD token here</body></html>`,
			want:  "",
			found: false,
		},
		{
			name:  "empty token",
			html:  `LSD",[],{"token":""},0]`,
			want:  "",
			found: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := lsdRe.FindStringSubmatch(tt.html)
			if !tt.found {
				if len(matches) >= 2 && matches[1] != "" {
					t.Errorf("expected no match, got %q", matches[1])
				}
				return
			}
			if len(matches) < 2 {
				t.Fatal("expected match, got none")
			}
			if matches[1] != tt.want {
				t.Errorf("token = %q, want %q", matches[1], tt.want)
			}
		})
	}
}
