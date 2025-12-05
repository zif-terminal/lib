package iface

import (
	"fmt"
	"time"
)

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
