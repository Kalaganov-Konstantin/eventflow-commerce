package resilience

import (
	"context"
	stderrors "errors"
	"testing"
	"time"
)

var errRetryable = stderrors.New("retryable")

func fastRetryConfig() RetryConfig {
	return RetryConfig{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: 5 * time.Millisecond}
}

func TestRetry_SucceedsWithoutRetryingOnFirstAttempt(t *testing.T) {
	calls := 0
	err := Retry(context.Background(), fastRetryConfig(), func() error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("Retry() error = %v", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}

func TestRetry_SucceedsAfterTransientFailures(t *testing.T) {
	calls := 0
	err := Retry(context.Background(), fastRetryConfig(), func() error {
		calls++
		if calls < 3 {
			return errRetryable
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Retry() error = %v", err)
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}
}

func TestRetry_StopsAfterMaxAttempts(t *testing.T) {
	calls := 0
	cfg := fastRetryConfig()
	cfg.MaxAttempts = 3

	err := Retry(context.Background(), cfg, func() error {
		calls++
		return errRetryable
	})
	if !stderrors.Is(err, errRetryable) {
		t.Fatalf("Retry() error = %v, want errRetryable", err)
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}
}

func TestRetry_MaxAttemptsBelowOneMeansNoRetries(t *testing.T) {
	calls := 0
	cfg := fastRetryConfig()
	cfg.MaxAttempts = 0

	if err := Retry(context.Background(), cfg, func() error {
		calls++
		return errRetryable
	}); !stderrors.Is(err, errRetryable) {
		t.Fatalf("Retry() error = %v, want errRetryable", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}

func TestRetry_StopsWhenPredicateRejectsError(t *testing.T) {
	errFatal := stderrors.New("not retryable")
	calls := 0
	cfg := fastRetryConfig()
	cfg.Retryable = func(err error) bool { return !stderrors.Is(err, errFatal) }

	err := Retry(context.Background(), cfg, func() error {
		calls++
		return errFatal
	})
	if !stderrors.Is(err, errFatal) {
		t.Fatalf("Retry() error = %v, want errFatal", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1 (predicate should stop further attempts)", calls)
	}
}

func TestRetry_StopsWhenContextIsCancelledBeforeStarting(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	calls := 0
	err := Retry(ctx, fastRetryConfig(), func() error {
		calls++
		return nil
	})
	if !stderrors.Is(err, context.Canceled) {
		t.Fatalf("Retry() error = %v, want context.Canceled", err)
	}
	if calls != 0 {
		t.Errorf("calls = %d, want 0", calls)
	}
}

func TestRetry_StopsWhenContextIsCancelledBetweenAttempts(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cfg := RetryConfig{MaxAttempts: 5, BaseDelay: 50 * time.Millisecond, MaxDelay: time.Second}

	calls := 0
	err := Retry(ctx, cfg, func() error {
		calls++
		if calls == 1 {
			cancel()
		}
		return errRetryable
	})
	if !stderrors.Is(err, context.Canceled) {
		t.Fatalf("Retry() error = %v, want context.Canceled", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}

func TestBackoffDelay_StaysWithinBounds(t *testing.T) {
	cfg := RetryConfig{BaseDelay: 10 * time.Millisecond, MaxDelay: 25 * time.Millisecond}

	for attempt := 1; attempt <= 10; attempt++ {
		delay := backoffDelay(cfg, attempt)
		if delay < 0 || delay > cfg.MaxDelay {
			t.Errorf("backoffDelay(attempt=%d) = %v, want within [0, %v]", attempt, delay, cfg.MaxDelay)
		}
	}
}

func TestBackoffDelay_DefaultsBaseDelayWhenUnset(t *testing.T) {
	delay := backoffDelay(RetryConfig{}, 1)
	if delay < 0 || delay > defaultBaseDelay {
		t.Errorf("backoffDelay() = %v, want within [0, %v]", delay, defaultBaseDelay)
	}
}
