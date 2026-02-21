package threads

import "fmt"

// errorClass categorizes HTTP error responses.
type errorClass int

const (
	errNone        errorClass = iota
	errRateLimited            // 429
	errForbidden              // 403 (IP ban or stale LSD)
	errNotFound               // 404
	errServerError            // 5xx
)

// classifyHTTPStatus maps an HTTP status code to an error class.
func classifyHTTPStatus(status int) errorClass {
	switch {
	case status >= 200 && status < 300:
		return errNone
	case status == 429:
		return errRateLimited
	case status == 403:
		return errForbidden
	case status == 404:
		return errNotFound
	case status >= 500:
		return errServerError
	default:
		return errNone
	}
}

// APIError represents a Threads API error with classification.
type APIError struct {
	Status  int
	Class   errorClass
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("threads: HTTP %d: %s", e.Status, e.Message)
}

// IsRateLimited returns true if the error is a rate limit response.
func IsRateLimited(err error) bool {
	if ae, ok := err.(*APIError); ok {
		return ae.Class == errRateLimited
	}
	return false
}

// IsForbidden returns true if the error is a 403 response.
func IsForbidden(err error) bool {
	if ae, ok := err.(*APIError); ok {
		return ae.Class == errForbidden
	}
	return false
}
