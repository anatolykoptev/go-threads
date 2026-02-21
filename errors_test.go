package threads

import (
	"errors"
	"testing"
)

func TestClassifyHTTPStatus(t *testing.T) {
	tests := []struct {
		status int
		want   errorClass
	}{
		{200, errNone},
		{201, errNone},
		{204, errNone},
		{429, errRateLimited},
		{403, errForbidden},
		{404, errNotFound},
		{500, errServerError},
		{502, errServerError},
		{503, errServerError},
		{400, errNone}, // unclassified
		{301, errNone}, // unclassified
	}

	for _, tt := range tests {
		got := classifyHTTPStatus(tt.status)
		if got != tt.want {
			t.Errorf("classifyHTTPStatus(%d) = %d, want %d", tt.status, got, tt.want)
		}
	}
}

func TestAPIError(t *testing.T) {
	err := &APIError{Status: 429, Class: errRateLimited, Message: "rate limited"}
	if err.Error() != "threads: HTTP 429: rate limited" {
		t.Errorf("Error() = %q", err.Error())
	}
	if !IsRateLimited(err) {
		t.Error("IsRateLimited = false, want true")
	}
	if IsForbidden(err) {
		t.Error("IsForbidden = true, want false")
	}

	err2 := &APIError{Status: 403, Class: errForbidden, Message: "forbidden"}
	if !IsForbidden(err2) {
		t.Error("IsForbidden = false, want true")
	}
	if IsRateLimited(err2) {
		t.Error("IsRateLimited = true, want false")
	}

	// Non-APIError
	plainErr := errors.New("some error")
	if IsRateLimited(plainErr) {
		t.Error("IsRateLimited(plainErr) = true")
	}
	if IsForbidden(plainErr) {
		t.Error("IsForbidden(plainErr) = true")
	}
}
