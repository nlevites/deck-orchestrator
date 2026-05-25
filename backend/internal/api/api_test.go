package api_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"deck-fleet/backend/internal/api"
	"deck-fleet/backend/internal/api/gen"
	"deck-fleet/backend/internal/testutil"
)

func TestRequestIDFromContext_emptyWhenNotSet(t *testing.T) {
	//nolint:staticcheck // SA1012: this test verifies the explicit nil-handling branch in RequestIDFromContext.
	require.Equal(t, "", api.RequestIDFromContext(nil))
	require.Equal(t, "", api.RequestIDFromContext(context.Background()))
}

func TestWithRequestID_roundtrips(t *testing.T) {
	ctx := api.WithRequestID(context.Background(), "test-id-123")
	require.Equal(t, "test-id-123", api.RequestIDFromContext(ctx))
}

func TestWriteJSON_setsContentTypeAndLength(t *testing.T) {
	rec := httptest.NewRecorder()
	api.WriteJSON(rec, http.StatusOK, struct {
		Foo string `json:"foo"`
	}{Foo: "bar"})

	require.Equal(t, "application/json; charset=utf-8", rec.Header().Get("Content-Type"))
	require.Equal(t, strconv.Itoa(rec.Body.Len()), rec.Header().Get("Content-Length"))
}

func TestWriteJSON_writesCorrectStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	api.WriteJSON(rec, http.StatusCreated, struct{}{})
	require.Equal(t, http.StatusCreated, rec.Code)
}

func TestWriteJSON_encodingErrorFallsBackTo500(t *testing.T) {
	rec := httptest.NewRecorder()
	api.WriteJSON(rec, http.StatusOK, make(chan int))
	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestDecodeAndValidate_happyPath(t *testing.T) {
	type req struct {
		Foo string `json:"foo"`
	}

	var got req
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		got, err = api.DecodeAndValidate[req](w, r)
		if err == nil {
			w.WriteHeader(http.StatusOK)
		}
	})

	rec := testutil.Do(t, h, http.MethodPost, "/", struct {
		Foo string `json:"foo"`
	}{Foo: "bar"})

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "bar", got.Foo)
}

func TestDecodeAndValidate_emptyBody_returns400InvalidJSON(t *testing.T) {
	type req struct {
		Foo string `json:"foo"`
	}

	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = api.DecodeAndValidate[req](w, r)
	})

	rec := testutil.Do(t, h, http.MethodPost, "/", nil)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	testutil.AssertErrorCode(t, rec, gen.ErrorCodeINVALIDJSON)
}

func TestDecodeAndValidate_invalidJSON_returns400(t *testing.T) {
	type req struct {
		Foo string `json:"foo"`
	}

	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = api.DecodeAndValidate[req](w, r)
	})

	rec := testutil.Do(t, h, http.MethodPost, "/", "not-valid-json{{{")
	require.Equal(t, http.StatusBadRequest, rec.Code)
	testutil.AssertErrorCode(t, rec, gen.ErrorCodeINVALIDJSON)
}

func TestDecodeAndValidate_unknownField_returns400SchemaViolation(t *testing.T) {
	type req struct {
		Foo string `json:"foo"`
	}

	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = api.DecodeAndValidate[req](w, r)
	})

	rec := testutil.Do(t, h, http.MethodPost, "/", `{"foo":"bar","unknown":"field"}`)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	testutil.AssertErrorCode(t, rec, gen.ErrorCodeSCHEMAVIOLATION)
}

func TestDecodeAndValidate_trailingData_returns400(t *testing.T) {
	type req struct {
		Foo string `json:"foo"`
	}

	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = api.DecodeAndValidate[req](w, r)
	})

	rec := testutil.Do(t, h, http.MethodPost, "/", `{"foo":"bar"}{"extra":"trailing"}`)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	testutil.AssertErrorCode(t, rec, gen.ErrorCodeINVALIDJSON)
}

func TestDecodeAndValidate_missingRequiredField_returns400SchemaViolation(t *testing.T) {
	type req struct {
		Name string `json:"name" validate:"required"`
	}

	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = api.DecodeAndValidate[req](w, r)
	})

	rec := testutil.Do(t, h, http.MethodPost, "/", `{}`)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	testutil.AssertErrorCode(t, rec, gen.ErrorCodeSCHEMAVIOLATION)
}

func TestDecodeAndValidate_bodyTooLarge_returns413(t *testing.T) {
	type req struct {
		Foo string `json:"foo"`
	}

	const limit = 5 // bytes — any real JSON body exceeds this
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, limit)
		_, _ = api.DecodeAndValidate[req](w, r)
	})

	rec := testutil.Do(t, h, http.MethodPost, "/", `{"foo":"this is far longer than five bytes"}`)
	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	testutil.AssertErrorCode(t, rec, gen.ErrorCodePAYLOADTOOLARGE)
}

func TestWriteSimpleError_setsCodeAndStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	api.WriteSimpleError(rec, req, gen.ErrorCodeINTERNALERROR, "boom")

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	testutil.AssertErrorCode(t, rec, gen.ErrorCodeINTERNALERROR)
}

func TestWriteDagValidationFailed_422withEntries(t *testing.T) {
	var entry gen.DagValidationFailedDetailEntry
	require.NoError(t, entry.FromDagHasNoJobsDetail(gen.DagHasNoJobsDetail{Code: gen.DAGHASNOJOBS}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	api.WriteDagValidationFailed(rec, req, []gen.DagValidationFailedDetailEntry{entry}, "validation failed")

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	inner := testutil.AssertErrorCode(t, rec, gen.ErrorCodeDAGVALIDATIONFAILED)
	details, ok := inner["details"].(map[string]any)
	require.True(t, ok, "expected details object")
	errors, ok := details["errors"].([]any)
	require.True(t, ok, "expected errors array")
	require.Len(t, errors, 1)
}

func TestWriteDuplicateResource_409(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	api.WriteDuplicateResource(rec, req, gen.Run{}, "already exists")

	require.Equal(t, http.StatusConflict, rec.Code)
	testutil.AssertErrorCode(t, rec, gen.ErrorCodeDUPLICATERESOURCE)
}

func TestWriteVersionMismatch_409withCurrentVersion(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	api.WriteVersionMismatch(rec, req, 42, gen.Run{Version: 42})

	require.Equal(t, http.StatusConflict, rec.Code)
	inner := testutil.AssertErrorCode(t, rec, gen.ErrorCodeVERSIONMISMATCH)
	details, ok := inner["details"].(map[string]any)
	require.True(t, ok, "expected details object")
	currentVersion, ok := details["current_version"].(float64)
	require.True(t, ok, "expected current_version field")
	require.Equal(t, float64(42), currentVersion)
}

func TestWriteAlreadyTerminal_409(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	api.WriteAlreadyTerminal(rec, req, gen.COMPLETED, gen.Run{}, "run is already terminal")

	require.Equal(t, http.StatusConflict, rec.Code)
	testutil.AssertErrorCode(t, rec, gen.ErrorCodeALREADYTERMINAL)
}

func TestWriteInvalidTransition_409(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	api.WriteInvalidTransition(rec, req, gen.DeckJobStatusFAILED, gen.Run{}, "cannot transition")

	require.Equal(t, http.StatusConflict, rec.Code)
	testutil.AssertErrorCode(t, rec, gen.ErrorCodeINVALIDTRANSITION)
}

func TestRequestID_generatesIDWhenAbsent(t *testing.T) {
	var capturedID string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedID = api.RequestIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	h := api.RequestID(inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	respID := rec.Header().Get("X-Request-ID")
	require.NotEmpty(t, respID)
	require.Equal(t, respID, capturedID)
	// UUIDv7 (and v4 fallback) are 36 chars: 8-4-4-4-12 hex with dashes
	require.Regexp(t, `^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`, respID)
}

func TestRequestID_preservesClientSuppliedID(t *testing.T) {
	var capturedID string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedID = api.RequestIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	h := api.RequestID(inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", "my-id")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, "my-id", rec.Header().Get("X-Request-ID"))
	require.Equal(t, "my-id", capturedID)
}

func TestRecover_catchesPanic_returns500(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("something went terribly wrong")
	})

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := api.Recover(logger)(inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	testutil.AssertErrorCode(t, rec, gen.ErrorCodeINTERNALERROR)
}

func TestBodyLimit_doesNotModifyBodyWhenLimitZero(t *testing.T) {
	originalBody := io.NopCloser(strings.NewReader("hello"))
	var gotBody io.ReadCloser

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody = r.Body
	})
	h := api.BodyLimit(0)(inner)

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Body = originalBody
	h.ServeHTTP(httptest.NewRecorder(), req)

	require.True(t, gotBody == originalBody, "body should be the same object when limit is 0")
}

func TestBodyLimit_doesNotModifyNilBody(t *testing.T) {
	var gotBody io.ReadCloser

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody = r.Body
	})
	h := api.BodyLimit(1024)(inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Body = nil // httptest sets Body to http.NoBody for nil readers; force it to nil
	h.ServeHTTP(httptest.NewRecorder(), req)

	require.Nil(t, gotBody)
}

func TestDegraded_blocksPostWhenFlagSet(t *testing.T) {
	var flag atomic.Bool
	flag.Store(true)

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := api.Degraded(&flag)(inner)

	rec := testutil.Do(t, h, http.MethodPost, "/api/runs", nil)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	testutil.AssertErrorCode(t, rec, gen.ErrorCodeDEGRADEDMODE)
}

func TestDegraded_allowsGetWhenFlagSet(t *testing.T) {
	var flag atomic.Bool
	flag.Store(true)

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := api.Degraded(&flag)(inner)

	rec := testutil.Do(t, h, http.MethodGet, "/api/runs", nil)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestDegraded_allowsMutationsWhenFlagClear(t *testing.T) {
	var flag atomic.Bool
	flag.Store(false)

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := api.Degraded(&flag)(inner)

	rec := testutil.Do(t, h, http.MethodPost, "/api/runs", nil)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestDegraded_allowsExecutorPostWhenFlagSet(t *testing.T) {
	var flag atomic.Bool
	flag.Store(true)

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := api.Degraded(&flag)(inner)

	// POST to /executor/events is not under /api/ so should not be blocked
	rec := testutil.Do(t, h, http.MethodPost, "/executor/events", nil)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestCORS_setsAllowOriginOnRequest(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := api.CORS([]string{"http://localhost:5173"})(inner)

	req := httptest.NewRequest(http.MethodGet, "/api/runs", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "http://localhost:5173", rec.Header().Get("Access-Control-Allow-Origin"))
}

func TestCORS_preflightReturns204(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("inner handler must not be called for OPTIONS preflight")
	})
	h := api.CORS([]string{"http://localhost:5173"})(inner)

	req := httptest.NewRequest(http.MethodOptions, "/api/runs", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Equal(t, "http://localhost:5173", rec.Header().Get("Access-Control-Allow-Origin"))
}

func TestLogging_doesNotPanic(t *testing.T) {
	var called bool
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := api.Logging(logger)(inner)

	req := httptest.NewRequest(http.MethodGet, "/api/runs", nil)
	rec := httptest.NewRecorder()

	require.NotPanics(t, func() {
		h.ServeHTTP(rec, req)
	})
	require.True(t, called, "inner handler should have been called")
}
