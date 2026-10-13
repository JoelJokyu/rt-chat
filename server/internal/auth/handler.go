package auth

import (
	"encoding/json"
	"errors"
	"net/http"
)

type AuthHandler struct {
	service *AuthService
}

func NewAuthHandler(service *AuthService) *AuthHandler {
	return &AuthHandler{
		service: service,
	}
}

func (h *AuthHandler) LoginUserHandler(w http.ResponseWriter, r *http.Request) {
	var input LoginInput

	err := json.NewDecoder(r.Body).Decode(&input)
	if err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	user, rawToken, err := h.service.Login(r.Context(), &input)

	if err != nil {
		if errors.Is(err, ErrInvalidInput) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if errors.Is(err, ErrInvalidCredentials) {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	cookie := &http.Cookie{
		Name:     "session_id",
		Value:    rawToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   false, // Set to true in production with HTTPS
		SameSite: http.SameSiteLaxMode,
		MaxAge:   72 * 60 * 60, // 72 hours
	}

	http.SetCookie(w, cookie)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(user)
}

func (h *AuthHandler) MeHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	session, ok := SessionFromContext(r.Context())
	if !ok {
		http.Error(
			w,
			"authentication required",
			http.StatusUnauthorized,
		)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	json.NewEncoder(w).Encode(map[string]int64{
		"user_id": session.UserID,
	})
}

func (h *AuthHandler) LogoutHandler(w http.ResponseWriter, r *http.Request) {
	session, ok := SessionFromContext(r.Context())
	if !ok {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}

	err := h.service.Logout(r.Context(), session.ID)

	// Name and Path must match the cookie created during login.
	http.SetCookie(w, &http.Cookie{
		Name:     "session_id",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   false, // Match login's setting; use true with HTTPS in production.
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})

	if err != nil {
		http.Error(w, "failed to log out", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
