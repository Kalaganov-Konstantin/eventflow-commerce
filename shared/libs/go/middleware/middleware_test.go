package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestRequestID_GeneratesWhenMissing(t *testing.T) {
	var gotID string
	handler := RequestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		gotID, _ = r.Context().Value(RequestIDKey).(string)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	headerID := rec.Header().Get("X-Request-ID")
	if headerID == "" {
		t.Fatal("X-Request-ID response header is empty")
	}
	if gotID != headerID {
		t.Errorf("context request id = %q, want %q (from response header)", gotID, headerID)
	}
}

func TestRequestID_ForwardsIncoming(t *testing.T) {
	const incomingID = "incoming-request-id"

	var gotID string
	handler := RequestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		gotID, _ = r.Context().Value(RequestIDKey).(string)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", incomingID)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if headerID := rec.Header().Get("X-Request-ID"); headerID != incomingID {
		t.Errorf("X-Request-ID header = %q, want %q", headerID, incomingID)
	}
	if gotID != incomingID {
		t.Errorf("context request id = %q, want %q", gotID, incomingID)
	}
}

func TestCORS_SetsHeadersAndCallsNext(t *testing.T) {
	nextCalled := false
	handler := CORS(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		nextCalled = true
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !nextCalled {
		t.Error("next handler was not called for a GET request")
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, "*")
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Error("Access-Control-Allow-Methods header is empty")
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got == "" {
		t.Error("Access-Control-Allow-Headers header is empty")
	}
}

func TestCORS_ShortCircuitsOptions(t *testing.T) {
	nextCalled := false
	handler := CORS(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		nextCalled = true
	}))

	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if nextCalled {
		t.Error("next handler was called for an OPTIONS request")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestLogging(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	logger := zap.New(core)

	handler := Logging(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))

	req := httptest.NewRequest(http.MethodGet, "/brew", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	entries := logs.All()
	if len(entries) != 1 {
		t.Fatalf("got %d log entries, want 1", len(entries))
	}

	fields := entries[0].ContextMap()
	if fields["method"] != http.MethodGet {
		t.Errorf("logged method = %v, want %v", fields["method"], http.MethodGet)
	}
	if fields["path"] != "/brew" {
		t.Errorf("logged path = %v, want %v", fields["path"], "/brew")
	}
	if fields["status_code"] != int64(http.StatusTeapot) {
		t.Errorf("logged status_code = %v, want %v", fields["status_code"], http.StatusTeapot)
	}
}

func TestRecovery_RecoversPanicAsInternalServerError(t *testing.T) {
	core, logs := observer.New(zap.ErrorLevel)
	logger := zap.New(core)

	handler := Recovery(logger)(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		panic("boom")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status code = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want %q", got, "application/json")
	}

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response body is not valid JSON: %v", err)
	}
	if body["error"] == "" {
		t.Error("response body has no error message")
	}

	if entries := logs.All(); len(entries) != 1 {
		t.Fatalf("got %d log entries, want 1", len(entries))
	}
}

func TestRecovery_PassesThroughWithoutPanic(t *testing.T) {
	logger := zap.NewNop()

	handler := Recovery(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != "ok" {
		t.Errorf("body = %q, want %q", rec.Body.String(), "ok")
	}
}

func TestTimeout_SetsDeadlineOnContext(t *testing.T) {
	handler := Timeout(50 * time.Millisecond)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if _, ok := r.Context().Deadline(); !ok {
			t.Error("request context has no deadline")
		}
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
}

func TestChain_AppliesMiddlewaresInOrder(t *testing.T) {
	var order []string

	marker := func(name string) Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, name)
				next.ServeHTTP(w, r)
			})
		}
	}

	final := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		order = append(order, "final")
	})

	handler := Chain(marker("first"), marker("second"), marker("third"))(final)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	want := []string{"first", "second", "third", "final"}
	if len(order) != len(want) {
		t.Fatalf("call order = %v, want %v", order, want)
	}
	for i, name := range want {
		if order[i] != name {
			t.Errorf("call order = %v, want %v", order, want)
			break
		}
	}
}
