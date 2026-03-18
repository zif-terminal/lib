package iface

import (
	"errors"
	"fmt"
	"time"
)

// ErrNotImplemented is returned by exchange methods that are defined in the
// interface but not yet implemented for a particular exchange. Callers can
// check for this with errors.Is to distinguish "not supported" from "no data".
var ErrNotImplemented = errors.New("not implemented for this exchange")

// RateLimitError indicates the exchange API rate limit was exceeded
type RateLimitError struct {
	Exchange   string
	Message    string
	RetryAfter time.Duration // Optional: when to retry (if exchange provides this)
}

func (e *RateLimitError) Error() string {
	if e.RetryAfter > 0 {
		return fmt.Sprintf("rate limit exceeded for %s: %s (retry after %v)",
			e.Exchange, e.Message, e.RetryAfter)
	}
	return fmt.Sprintf("rate limit exceeded for %s: %s", e.Exchange, e.Message)
}

// IsRateLimitError checks if an error is a RateLimitError
func IsRateLimitError(err error) bool {
	_, ok := err.(*RateLimitError)
	return ok
}
