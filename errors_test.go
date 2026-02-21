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

	// Login redirect
	err3 := &APIError{Status: 200, Class: errLoginRedirect, Message: "login redirect"}
	if !IsLoginRedirect(err3) {
		t.Error("IsLoginRedirect = false, want true")
	}
	if IsRateLimited(err3) {
		t.Error("IsRateLimited(loginRedirect) = true, want false")
	}

	// Non-APIError
	plainErr := errors.New("some error")
	if IsRateLimited(plainErr) {
		t.Error("IsRateLimited(plainErr) = true")
	}
	if IsForbidden(plainErr) {
		t.Error("IsForbidden(plainErr) = true")
	}
	if IsLoginRedirect(plainErr) {
		t.Error("IsLoginRedirect(plainErr) = true")
	}
}

func TestIsLoginRedirect(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{"accounts login path", `<html><meta http-equiv="refresh" content="0;url=/accounts/login"/></html>`, true},
		{"require_login json", `{"require_login":true,"status":"fail"}`, true},
		{"normal page", `<html><body>Hello Threads</body></html>`, false},
		{"empty body", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isLoginRedirect([]byte(tt.body))
			if got != tt.want {
				t.Errorf("isLoginRedirect = %v, want %v", got, tt.want)
			}
		})
	}
}
