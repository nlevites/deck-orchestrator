package api

import "context"

// ctxKey is the unexported type used as context key. Using a typed key
// prevents accidental collisions with strings other packages might use.
type ctxKey int

const (
	ctxKeyRequestID ctxKey = iota + 1
)

func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKeyRequestID, id)
}

// RequestIDFromContext returns the request ID from ctx, or "" outside HTTP
// requests (e.g. background goroutines without propagated context).
func RequestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(ctxKeyRequestID).(string); ok {
		return v
	}
	return ""
}
