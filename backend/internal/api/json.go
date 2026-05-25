package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
)

// WriteJSON encodes body as JSON with the supplied status.
// Buffers first so a marshal error doesn't leave half a response on the wire.
// API.md §2 requires explicit Content-Length (not chunked) on normal endpoints.
//
// body is plain any rather than generic: json.Encoder takes any anyway.
func WriteJSON(w http.ResponseWriter, status int, body any) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		// Own response shapes should not fail to encode; access log captures the panic path.
		http.Error(w, "encode error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}
