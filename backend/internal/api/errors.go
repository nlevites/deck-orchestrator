package api

import (
	"net/http"

	"deck-fleet/backend/internal/api/gen"
)

// errEnvelope wraps concrete variant structs in `"error"` instead of gen.Error's
// discriminated union — variants already carry the right JSON shape (API.md §4).
type errEnvelope struct {
	Error any `json:"error"`
}

// Exhaustive map from closed error codes to HTTP status (API.md §11).
func codeStatus(code gen.ErrorCode) int {
	switch code {
	case gen.ErrorCodeINVALIDJSON,
		gen.ErrorCodeSCHEMAVIOLATION,
		gen.ErrorCodeMISSINGVERSION,
		gen.ErrorCodeINVALIDRESOLUTION:
		return http.StatusBadRequest
	case gen.ErrorCodeRUNNOTFOUND,
		gen.ErrorCodeJOBNOTFOUND,
		gen.ErrorCodeUNKNOWNATTEMPT,
		gen.ErrorCodeDECKNOTFOUND:
		return http.StatusNotFound
	case gen.ErrorCodeDECKDECOMMISSIONED:
		return http.StatusGone
	case gen.ErrorCodeEXECUTORUNREACHABLE:
		return http.StatusBadGateway
	case gen.ErrorCodeVERSIONMISMATCH,
		gen.ErrorCodeDUPLICATERESOURCE,
		gen.ErrorCodeALREADYTERMINAL,
		gen.ErrorCodeINVALIDTRANSITION:
		return http.StatusConflict
	case gen.ErrorCodePAYLOADTOOLARGE:
		return http.StatusRequestEntityTooLarge
	case gen.ErrorCodeDAGVALIDATIONFAILED:
		return http.StatusUnprocessableEntity
	case gen.ErrorCodeINTERNALERROR:
		return http.StatusInternalServerError
	case gen.ErrorCodeDEGRADEDMODE:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

func WriteSimpleError(w http.ResponseWriter, r *http.Request, code gen.ErrorCode, message string) {
	requestID := RequestIDFromContext(r.Context())
	body := struct {
		Code      gen.ErrorCode `json:"code"`
		Message   string        `json:"message"`
		RequestID string        `json:"request_id"`
	}{Code: code, Message: message, RequestID: requestID}
	WriteJSON(w, codeStatus(code), errEnvelope{Error: body})
}

// Embeds current run in details so clients can re-render without a follow-up GET (API.md §6).
func WriteVersionMismatch(w http.ResponseWriter, r *http.Request, currentVersion int64, currentState gen.Run) {
	requestID := RequestIDFromContext(r.Context())
	var v gen.VersionMismatchError
	v.Code = gen.VersionMismatchErrorCode(gen.ErrorCodeVERSIONMISMATCH)
	v.Message = "Run version has moved since you last read it."
	v.RequestId = requestID
	v.Details.CurrentVersion = currentVersion
	v.Details.CurrentState = currentState
	WriteJSON(w, http.StatusConflict, errEnvelope{Error: v})
}

// Cancel on terminal run (§8.4); retry when parent run is terminal (§8.5).
func WriteAlreadyTerminal(w http.ResponseWriter, r *http.Request, currentStatus gen.RunStatus, currentState gen.Run, message string) {
	requestID := RequestIDFromContext(r.Context())
	var v gen.AlreadyTerminalError
	v.Code = gen.AlreadyTerminalErrorCode(gen.ErrorCodeALREADYTERMINAL)
	v.Message = message
	v.RequestId = requestID
	v.Details.CurrentStatus = currentStatus
	v.Details.CurrentState = currentState
	WriteJSON(w, http.StatusConflict, errEnvelope{Error: v})
}

// Retry on non-FAILED job; resolve on non-AMBIGUOUS job (§8.5, §8.6).
// currentStatus is the DeckJob's status, not the run's (API.md §11).
func WriteInvalidTransition(w http.ResponseWriter, r *http.Request, currentStatus gen.DeckJobStatus, currentState gen.Run, message string) {
	requestID := RequestIDFromContext(r.Context())
	var v gen.InvalidTransitionError
	v.Code = gen.InvalidTransitionErrorCode(gen.ErrorCodeINVALIDTRANSITION)
	v.Message = message
	v.RequestId = requestID
	v.Details.CurrentStatus = currentStatus
	v.Details.CurrentState = currentState
	WriteJSON(w, http.StatusConflict, errEnvelope{Error: v})
}

// Submit against existing run id (API.md §5 Pattern A).
func WriteDuplicateResource(w http.ResponseWriter, r *http.Request, currentState gen.Run, message string) {
	requestID := RequestIDFromContext(r.Context())
	var v gen.DuplicateResourceError
	v.Code = gen.DuplicateResourceErrorCode(gen.ErrorCodeDUPLICATERESOURCE)
	v.Message = message
	v.RequestId = requestID
	v.Details.CurrentState = currentState
	WriteJSON(w, http.StatusConflict, errEnvelope{Error: v})
}

// API.md §8.1.
func WriteDagValidationFailed(w http.ResponseWriter, r *http.Request, entries []gen.DagValidationFailedDetailEntry, message string) {
	requestID := RequestIDFromContext(r.Context())
	var v gen.DagValidationFailedError
	v.Code = gen.DagValidationFailedErrorCode(gen.ErrorCodeDAGVALIDATIONFAILED)
	v.Message = message
	v.RequestId = requestID
	v.Details.Errors = entries
	WriteJSON(w, http.StatusUnprocessableEntity, errEnvelope{Error: v})
}
