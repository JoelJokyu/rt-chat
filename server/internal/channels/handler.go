package channels

import (
	"encoding/json"
	"errors"
	"net/http"
	"rtchat/internal/auth"
	"strconv"
)

type ChannelHandler struct {
	service *ChannelService
}

func NewChannelHandler(service *ChannelService) *ChannelHandler {
	return &ChannelHandler{
		service: service,
	}
}

func (h *ChannelHandler) CreateChannelHandler(w http.ResponseWriter, r *http.Request) {
	session, ok := auth.SessionFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var input CreateChannelInput

	serverID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid server ID", http.StatusBadRequest)
		return
	}

	err = json.NewDecoder(r.Body).Decode(&input)
	if err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	channel, err := h.service.Create(r.Context(), serverID, &input, session.UserID)
	if err != nil {
		if errors.Is(err, ErrInvalidInput) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if errors.Is(err, ErrForbidden) {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		http.Error(w, "failed to create channel", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)

	json.NewEncoder(w).Encode(channel)
}

func (h *ChannelHandler) ListForMemberHandler(w http.ResponseWriter, r *http.Request) {
	session, ok := auth.SessionFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	serverID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid server ID", http.StatusBadRequest)
		return
	}

	channels, err := h.service.ListForMember(r.Context(), serverID, session.UserID)
	if err != nil {
		if errors.Is(err, ErrInvalidInput) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if errors.Is(err, ErrForbidden) {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		http.Error(w, "failed to get channels", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	json.NewEncoder(w).Encode(channels)
}
