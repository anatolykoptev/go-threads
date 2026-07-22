package threads

import (
	"fmt"
	"testing"
)

func TestComputeJazoest(t *testing.T) {
	// "2" + sum(charcodes) : a=97 b=98 c=99 -> 294
	if got := computeJazoest("abc"); got != "2294" {
		t.Errorf("computeJazoest(abc) = %q, want 2294", got)
	}
	if got := computeJazoest(""); got != "20" {
		t.Errorf("computeJazoest(empty) = %q, want 20", got)
	}
}

func TestIsRateLimitedForbidden_WrappedError(t *testing.T) {
	// doGraphQL/fetchPage wrap the terminal error with %w — predicates must unwrap.
	rl := fmt.Errorf("search failed after 3 attempts: %w", &APIError{Status: 429, Class: errRateLimited})
	if !IsRateLimited(rl) {
		t.Error("IsRateLimited must detect a WRAPPED 429 APIError")
	}
	fb := fmt.Errorf("page failed: %w", &APIError{Status: 403, Class: errForbidden})
	if !IsForbidden(fb) {
		t.Error("IsForbidden must detect a WRAPPED 403 APIError")
	}
}
