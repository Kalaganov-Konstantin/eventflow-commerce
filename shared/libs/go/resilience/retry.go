package resilience

import (
	"context"
	"math/rand"
	"time"
)

// maxBackoffShift bounds the exponent used to compute backoff delay, so a large attempt number
// cannot overflow the shift into a negative duration.
const maxBackoffShift = 20

// defaultBaseDelay is used when RetryConfig.BaseDelay is not positive.
const defaultBaseDelay = 100 * time.Millisecond

// RetryConfig controls Retry's attempt limit, backoff schedule and retry predicate.
type RetryConfig struct {
	// MaxAttempts is the total number of times fn may run, including the first attempt. Values
	// below 1 are treated as 1, i.e. no retries.
	MaxAttempts int
	// BaseDelay is the backoff delay before the second attempt; each later attempt doubles it. A
	// non-positive BaseDelay falls back to 100ms.
	BaseDelay time.Duration
	// MaxDelay caps the computed backoff delay before jitter is applied. A non-positive MaxDelay
	// leaves the delay uncapped.
	MaxDelay time.Duration
	// Retryable reports whether err should be retried. A nil Retryable retries every non-nil
	// error.
	Retryable func(error) bool
}

// Retry runs fn, retrying with exponential backoff and full jitter between attempts until fn
// succeeds, cfg.Retryable rejects the error, cfg.MaxAttempts is exhausted, or ctx is done.
//
// Retry is only safe to wrap around idempotent operations: on a timeout or a dropped response the
// previous attempt may have already taken effect on the remote side even though it reported
// failure, so a retried call must tolerate running again.
func Retry(ctx context.Context, cfg RetryConfig, fn func() error) error {
	maxAttempts := cfg.MaxAttempts
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		lastErr = fn()
		if lastErr == nil {
			return nil
		}
		if cfg.Retryable != nil && !cfg.Retryable(lastErr) {
			return lastErr
		}
		if attempt == maxAttempts {
			break
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoffDelay(cfg, attempt)):
		}
	}
	return lastErr
}

// backoffDelay computes the jittered exponential backoff before the attempt after attempt,
// picking uniformly in [0, min(base*2^(attempt-1), maxDelay)] (full jitter).
func backoffDelay(cfg RetryConfig, attempt int) time.Duration {
	base := cfg.BaseDelay
	if base <= 0 {
		base = defaultBaseDelay
	}

	shift := attempt - 1
	if shift > maxBackoffShift {
		shift = maxBackoffShift
	}
	delay := base * time.Duration(int64(1)<<uint(shift))

	if cfg.MaxDelay > 0 && delay > cfg.MaxDelay {
		delay = cfg.MaxDelay
	}
	if delay <= 0 {
		return 0
	}
	return time.Duration(rand.Int63n(int64(delay) + 1))
}
