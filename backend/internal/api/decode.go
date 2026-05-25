package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/go-playground/validator/v10"

	"deck-fleet/backend/internal/api/gen"
)

// validate is the package-singleton validator. validator caches reflection
// data per Go type, so reusing one instance across the process is the
// recommended pattern.
var validate = validator.New(validator.WithRequiredStructEnabled())

// DecodeAndValidate decodes JSON into T and runs struct-tag validation.
// On failure the response is already written; returns zero T and the error.
func DecodeAndValidate[T any](w http.ResponseWriter, r *http.Request) (T, error) {
	var body T

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeDecodeError(w, r, err)
		return body, err
	}

	if err := dec.Decode(new(any)); !errors.Is(err, io.EOF) {
		WriteSimpleError(w, r, gen.ErrorCodeINVALIDJSON, "trailing data after JSON body")
		return body, fmt.Errorf("trailing data: %w", err)
	}

	if err := validate.Struct(body); err != nil {
		writeValidationError(w, r, err)
		return body, err
	}
	return body, nil
}

func writeDecodeError(w http.ResponseWriter, r *http.Request, err error) {
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		WriteSimpleError(w, r, gen.ErrorCodePAYLOADTOOLARGE,
			fmt.Sprintf("request body exceeds %d bytes", maxBytesErr.Limit))
		return
	}

	var syntaxErr *json.SyntaxError
	var unmarshalTypeErr *json.UnmarshalTypeError
	switch {
	case errors.As(err, &syntaxErr):
		WriteSimpleError(w, r, gen.ErrorCodeINVALIDJSON,
			fmt.Sprintf("invalid JSON at byte %d: %s", syntaxErr.Offset, syntaxErr.Error()))
	case errors.As(err, &unmarshalTypeErr):
		WriteSimpleError(w, r, gen.ErrorCodeSCHEMAVIOLATION,
			fmt.Sprintf("field %q has wrong type: expected %s", unmarshalTypeErr.Field, unmarshalTypeErr.Type))
	case strings.HasPrefix(err.Error(), "json: unknown field"):
		WriteSimpleError(w, r, gen.ErrorCodeSCHEMAVIOLATION, err.Error())
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		WriteSimpleError(w, r, gen.ErrorCodeINVALIDJSON, "empty or truncated request body")
	default:
		WriteSimpleError(w, r, gen.ErrorCodeINVALIDJSON, err.Error())
	}
}

func writeValidationError(w http.ResponseWriter, r *http.Request, err error) {
	var validationErrs validator.ValidationErrors
	if !errors.As(err, &validationErrs) {
		WriteSimpleError(w, r, gen.ErrorCodeSCHEMAVIOLATION, err.Error())
		return
	}
	var b strings.Builder
	b.WriteString("validation failed: ")
	for i, fe := range validationErrs {
		if i > 0 {
			b.WriteString("; ")
		}
		fmt.Fprintf(&b, "field %q failed %q (got %v)", fe.Field(), fe.Tag(), fe.Value())
	}
	WriteSimpleError(w, r, gen.ErrorCodeSCHEMAVIOLATION, b.String())
}
