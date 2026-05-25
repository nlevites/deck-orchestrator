package testutil

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"deck-fleet/backend/internal/api/gen"
)

// Do dispatches against h. body: nil, []byte, string, io.Reader, or JSON-marshalable value.
func Do(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var (
		reader   io.Reader
		jsonBody bool
	)
	switch v := body.(type) {
	case nil:
		reader = nil
	case []byte:
		reader = bytes.NewReader(v)
	case string:
		reader = strings.NewReader(v)
	case io.Reader:
		reader = v
	default:
		buf, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("testutil: marshal body: %v", err)
		}
		reader = bytes.NewReader(buf)
		jsonBody = true
	}
	req := httptest.NewRequest(method, path, reader)
	if jsonBody {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func DecodeJSON(t *testing.T, rec *httptest.ResponseRecorder, dst any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), dst); err != nil {
		t.Fatalf("testutil: decode response: %v\nbody: %s", err, rec.Body.String())
	}
}

// errEnvelope mirrors wire error shape without importing api's unexported type.
type errEnvelope struct {
	Error map[string]any `json:"error"`
}

// AssertErrorCode checks the wire error envelope code; returns the inner error map.
func AssertErrorCode(t *testing.T, rec *httptest.ResponseRecorder, want gen.ErrorCode) map[string]any {
	t.Helper()
	var env errEnvelope
	DecodeJSON(t, rec, &env)
	if env.Error == nil {
		t.Fatalf("testutil: response is not an error envelope: %s", rec.Body.String())
	}
	got, _ := env.Error["code"].(string)
	if got != string(want) {
		t.Fatalf("testutil: error code = %q, want %q (status=%d, body=%s)",
			got, want, rec.Code, rec.Body.String())
	}
	return env.Error
}

// AssertDagValidationCodes checks 422 DAG_VALIDATION_FAILED detail codes (order-insensitive).
func AssertDagValidationCodes(t *testing.T, rec *httptest.ResponseRecorder, want ...gen.DagValidationCode) {
	t.Helper()
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("testutil: status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
	outer := AssertErrorCode(t, rec, gen.ErrorCodeDAGVALIDATIONFAILED)
	details, ok := outer["details"].(map[string]any)
	if !ok {
		t.Fatalf("testutil: missing details: %s", rec.Body.String())
	}
	rawErrs, ok := details["errors"].([]any)
	if !ok {
		t.Fatalf("testutil: details.errors not array: %s", rec.Body.String())
	}
	gotCounts := map[string]int{}
	for _, e := range rawErrs {
		obj, _ := e.(map[string]any)
		code, _ := obj["code"].(string)
		gotCounts[code]++
	}
	wantCounts := map[string]int{}
	for _, c := range want {
		wantCounts[string(c)]++
	}
	if !equalCountMap(gotCounts, wantCounts) {
		t.Fatalf("testutil: dag validation codes = %v, want %v\nbody=%s",
			gotCounts, wantCounts, rec.Body.String())
	}
}

func equalCountMap(a, b map[string]int) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
