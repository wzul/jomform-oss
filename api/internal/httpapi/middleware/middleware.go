package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

type ctxKey int

const (
	requestIDKey ctxKey = iota
	accountIDKey
)

// RequestID assigns a unique ID per request and echoes it in X-Request-ID.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			b := make([]byte, 8)
			_, _ = rand.Read(b)
			id = hex.EncodeToString(b)
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey, id)))
	})
}

// GetRequestID returns the current request ID from context.
func GetRequestID(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDKey).(string); ok {
		return v
	}
	return ""
}

// WithAccountID returns a context carrying the resolved account ID so the
// logger and error paths can correlate logs by account.
func WithAccountID(ctx context.Context, accountID string) context.Context {
	return context.WithValue(ctx, accountIDKey, accountID)
}

// GetAccountID returns the resolved account ID from context ("" when anon).
func GetAccountID(ctx context.Context) string {
	if v, ok := ctx.Value(accountIDKey).(string); ok {
		return v
	}
	return ""
}
