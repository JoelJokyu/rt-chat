package users

import (
	"encoding/json"
	"errors"
	"net/http"
)

type UserHandler struct {
	service *UserService
}

func NewUserHandler(service *UserService) *UserHandler {
	return &UserHandler{
		service: service,
	}
}

func (h *UserHandler) RegisterUserHandler(w http.ResponseWriter, r *http.Request) {
	var input RegisterInput

	// Decode JSON request body
	err := json.NewDecoder(r.Body).Decode(&input)
	if err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Call service
	user, err := h.service.Register(r.Context(), &input)
	if err != nil {
		// Handle specific errors (e.g., validation, duplicate user)
		if errors.Is(err, ErrInvalidInput) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if errors.Is(err, ErrEmailTaken) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(user)
}
