package auth

import (
	"context"
	"net/http"
)

type contextKey string

const sessionContextKey contextKey = "session"

func AuthMiddleware(sessionService *SessionService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie("session_id")
			if err != nil {
				http.Error(w, "authentication required", http.StatusUnauthorized)
				return
			}

			session, err := AuthenticateSession(r.Context(), sessionService, cookie.Value)
			if err != nil {
				http.Error(w, "invalid or expired session", http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), sessionContextKey, session)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func SessionFromContext(ctx context.Context) (*Session, bool) {
	session, ok := ctx.Value(sessionContextKey).(*Session)
	return session, ok
}
