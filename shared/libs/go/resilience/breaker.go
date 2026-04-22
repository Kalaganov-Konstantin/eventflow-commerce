// Package resilience wraps remote calls with a circuit breaker and retry helper shared by every
// service that talks to another service over the network.
package resilience

import (
	"errors"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/sony/gobreaker/v2"
)

// ErrOpen is returned by Execute when the circuit is open and the call was rejected without
// running.
var ErrOpen = errors.New("circuit breaker is open")

// breakerState and breakerFailures are registered once per process and shared by every Breaker,
// distinguished by the "name" label, so constructing many breakers never re-registers a
// collector.
var (
	breakerState = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "circuit_breaker_state",
		Help: "Current circuit breaker state: 0 closed, 1 half-open, 2 open.",
	}, []string{"name"})

	breakerFailures = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "circuit_breaker_failures_total",
		Help: "Total number of calls a circuit breaker recorded as failed.",
	}, []string{"name"})
)

// Config controls when a Breaker trips open and how long it stays open before probing again.
type Config struct {
	// Name identifies the breaker in metrics and in the error Execute returns while open.
	Name string
	// FailureThreshold is the number of consecutive failures in the closed state that trips the
	// breaker open. A non-positive value disables tripping.
	FailureThreshold uint32
	// Window resets the closed-state failure count once it elapses with no new failures. A
	// non-positive Window never resets it.
	Window time.Duration
	// OpenTimeout is how long the breaker stays open before letting a single probe request
	// through in the half-open state. A non-positive value uses gobreaker's 60 second default.
	OpenTimeout time.Duration
}

// Breaker wraps a gobreaker circuit breaker, exposing its state and failure count as Prometheus
// metrics labeled by name.
type Breaker struct {
	name string
	cb   *gobreaker.CircuitBreaker[any]
}

// NewBreaker builds a Breaker configured by cfg.
func NewBreaker(cfg Config) *Breaker {
	breakerState.WithLabelValues(cfg.Name).Set(float64(gobreaker.StateClosed))

	cb := gobreaker.NewCircuitBreaker[any](gobreaker.Settings{
		Name:        cfg.Name,
		MaxRequests: 1,
		Interval:    cfg.Window,
		Timeout:     cfg.OpenTimeout,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return cfg.FailureThreshold > 0 && counts.ConsecutiveFailures >= cfg.FailureThreshold
		},
		IsSuccessful: func(err error) bool {
			success := err == nil
			if !success {
				breakerFailures.WithLabelValues(cfg.Name).Inc()
			}
			return success
		},
		OnStateChange: func(name string, _, to gobreaker.State) {
			breakerState.WithLabelValues(name).Set(float64(to))
		},
	})

	return &Breaker{name: cfg.Name, cb: cb}
}

// State returns the breaker's current state.
func (b *Breaker) State() gobreaker.State {
	return b.cb.State()
}

// Execute runs fn if the circuit allows it. If the circuit is open, fn does not run and Execute
// returns ErrOpen wrapped with the breaker's name.
func Execute[T any](b *Breaker, fn func() (T, error)) (T, error) {
	result, err := b.cb.Execute(func() (any, error) {
		return fn()
	})
	if err != nil {
		var zero T
		if errors.Is(err, gobreaker.ErrOpenState) || errors.Is(err, gobreaker.ErrTooManyRequests) {
			return zero, fmt.Errorf("%s: %w", b.name, ErrOpen)
		}
		return zero, err
	}
	return result.(T), nil
}
