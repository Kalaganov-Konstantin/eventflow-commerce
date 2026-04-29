package resilience

import (
	stderrors "errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/sony/gobreaker/v2"
)

var errBoom = stderrors.New("boom")

func succeed() (int, error) { return 1, nil }
func fail() (int, error)    { return 0, errBoom }

func TestBreaker_ExecuteSucceedsWhenClosed(t *testing.T) {
	b := NewBreaker(Config{Name: t.Name(), FailureThreshold: 2})

	got, err := Execute(b, succeed)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got != 1 {
		t.Errorf("Execute() = %d, want 1", got)
	}
	if b.State() != gobreaker.StateClosed {
		t.Errorf("State() = %v, want closed", b.State())
	}
}

func TestBreaker_TripsOpenAfterConsecutiveFailures(t *testing.T) {
	b := NewBreaker(Config{Name: t.Name(), FailureThreshold: 2})

	for i := 0; i < 2; i++ {
		if _, err := Execute(b, fail); !stderrors.Is(err, errBoom) {
			t.Fatalf("Execute() call %d error = %v, want errBoom", i, err)
		}
	}

	if b.State() != gobreaker.StateOpen {
		t.Fatalf("State() = %v, want open after %d consecutive failures", b.State(), 2)
	}

	calls := 0
	_, err := Execute(b, func() (int, error) {
		calls++
		return 1, nil
	})
	if !stderrors.Is(err, ErrOpen) {
		t.Fatalf("Execute() error = %v, want ErrOpen", err)
	}
	if calls != 0 {
		t.Errorf("underlying function ran %d times while the circuit was open, want 0", calls)
	}
}

func TestBreaker_HalfOpenProbeClosesOnSuccess(t *testing.T) {
	b := NewBreaker(Config{
		Name:             t.Name(),
		FailureThreshold: 1,
		OpenTimeout:      20 * time.Millisecond,
	})

	if _, err := Execute(b, fail); !stderrors.Is(err, errBoom) {
		t.Fatalf("Execute() error = %v, want errBoom", err)
	}
	if b.State() != gobreaker.StateOpen {
		t.Fatalf("State() = %v, want open", b.State())
	}

	time.Sleep(30 * time.Millisecond)

	if got, err := Execute(b, succeed); err != nil || got != 1 {
		t.Fatalf("Execute() half-open probe = (%d, %v), want (1, nil)", got, err)
	}
	if b.State() != gobreaker.StateClosed {
		t.Fatalf("State() = %v, want closed after a successful half-open probe", b.State())
	}
}

func TestBreaker_HalfOpenProbeReopensOnFailure(t *testing.T) {
	b := NewBreaker(Config{
		Name:             t.Name(),
		FailureThreshold: 1,
		OpenTimeout:      20 * time.Millisecond,
	})

	if _, err := Execute(b, fail); !stderrors.Is(err, errBoom) {
		t.Fatalf("Execute() error = %v, want errBoom", err)
	}

	time.Sleep(30 * time.Millisecond)

	if _, err := Execute(b, fail); !stderrors.Is(err, errBoom) {
		t.Fatalf("Execute() half-open probe error = %v, want errBoom", err)
	}
	if b.State() != gobreaker.StateOpen {
		t.Fatalf("State() = %v, want open again after a failed half-open probe", b.State())
	}
}

func TestBreaker_RecordsStateAndFailureMetrics(t *testing.T) {
	name := t.Name()
	b := NewBreaker(Config{Name: name, FailureThreshold: 1, OpenTimeout: time.Hour})

	if got := testutil.ToFloat64(breakerState.WithLabelValues(name)); got != float64(gobreaker.StateClosed) {
		t.Errorf("initial state metric = %v, want closed (0)", got)
	}

	if _, err := Execute(b, fail); !stderrors.Is(err, errBoom) {
		t.Fatalf("Execute() error = %v, want errBoom", err)
	}

	if got := testutil.ToFloat64(breakerFailures.WithLabelValues(name)); got != 1 {
		t.Errorf("failure counter = %v, want 1", got)
	}
	if got := testutil.ToFloat64(breakerState.WithLabelValues(name)); got != float64(gobreaker.StateOpen) {
		t.Errorf("state metric = %v, want open (%v)", got, float64(gobreaker.StateOpen))
	}
}

func TestExecute_PreservesUnderlyingError(t *testing.T) {
	b := NewBreaker(Config{Name: t.Name(), FailureThreshold: 5})

	_, err := Execute(b, fail)
	if !stderrors.Is(err, errBoom) {
		t.Errorf("Execute() error = %v, want it to wrap errBoom", err)
	}
	if stderrors.Is(err, ErrOpen) {
		t.Error("Execute() error should not be ErrOpen while the circuit is closed")
	}
}
