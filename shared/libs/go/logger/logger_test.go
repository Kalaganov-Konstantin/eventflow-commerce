package logger

import (
	"context"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func testConfig(level, environment string) Config {
	return Config{
		Level:       level,
		Environment: environment,
		Service:     "test-service",
		Version:     "1.0.0",
	}
}

func TestNew_Development(t *testing.T) {
	l, err := New(testConfig("debug", "development"))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if !l.Core().Enabled(zapcore.DebugLevel) {
		t.Error("debug level should be enabled")
	}
}

func TestNew_Production(t *testing.T) {
	l, err := New(testConfig("warn", "production"))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if l.Core().Enabled(zapcore.InfoLevel) {
		t.Error("info level should not be enabled when configured level is warn")
	}
	if !l.Core().Enabled(zapcore.WarnLevel) {
		t.Error("warn level should be enabled")
	}
}

func TestNew_InvalidLevelDefaultsToInfo(t *testing.T) {
	l, err := New(testConfig("not-a-real-level", "development"))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if l.Core().Enabled(zapcore.DebugLevel) {
		t.Error("debug level should not be enabled when falling back to info")
	}
	if !l.Core().Enabled(zapcore.InfoLevel) {
		t.Error("info level should be enabled as the fallback")
	}
}

func newObservedLogger() (*Logger, *observer.ObservedLogs) {
	core, logs := observer.New(zap.DebugLevel)
	zl := zap.New(core)
	return &Logger{Logger: zl, sugar: zl.Sugar()}, logs
}

func fieldValue(t *testing.T, logs *observer.ObservedLogs, key string) interface{} {
	t.Helper()
	entries := logs.All()
	if len(entries) != 1 {
		t.Fatalf("got %d log entries, want 1", len(entries))
	}
	fields := entries[0].ContextMap()
	value, ok := fields[key]
	if !ok {
		t.Fatalf("log entry has no field %q, got %v", key, fields)
	}
	return value
}

func TestWithCorrelationID(t *testing.T) {
	l, logs := newObservedLogger()
	l.WithCorrelationID("corr-1").Info("event")

	if got := fieldValue(t, logs, "correlation_id"); got != "corr-1" {
		t.Errorf("correlation_id = %v, want %v", got, "corr-1")
	}
}

func TestWithUserID(t *testing.T) {
	l, logs := newObservedLogger()
	l.WithUserID("user-1").Info("event")

	if got := fieldValue(t, logs, "user_id"); got != "user-1" {
		t.Errorf("user_id = %v, want %v", got, "user-1")
	}
}

func TestWithRequestID(t *testing.T) {
	l, logs := newObservedLogger()
	l.WithRequestID("req-1").Info("event")

	if got := fieldValue(t, logs, "request_id"); got != "req-1" {
		t.Errorf("request_id = %v, want %v", got, "req-1")
	}
}

func TestWithTracing_NoActiveSpan(t *testing.T) {
	l, logs := newObservedLogger()
	l.WithTracing(context.Background()).Info("event")

	entries := logs.All()
	if len(entries) != 1 {
		t.Fatalf("got %d log entries, want 1", len(entries))
	}
	fields := entries[0].ContextMap()
	if _, ok := fields["trace_id"]; ok {
		t.Error("trace_id should not be set without an active span")
	}
	if _, ok := fields["span_id"]; ok {
		t.Error("span_id should not be set without an active span")
	}
}
