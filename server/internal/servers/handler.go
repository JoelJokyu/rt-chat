package servers

import (
	"encoding/json"
	"errors"
	"net/http"
	"rtchat/internal/auth"
	"strconv"
)

type ServerHandler struct {
	service *ServerService
}

func NewServerHandler(service *ServerService) *ServerHandler {
	return &ServerHandler{
		service: service,
	}
}

func parseIDFromRequest(r *http.Request) (int64, error) {
	rawID := r.PathValue("id")

	if rawID == "" {
		return 0, errors.New("missing server ID")
	}

	return strconv.ParseInt(rawID, 10, 64)
}

func (h *ServerHandler) CreateServerHandler(w http.ResponseWriter, r *http.Request) {
	session, ok := auth.SessionFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var input CreateServerInput

	err := json.NewDecoder(r.Body).Decode(&input)
	if err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	server, err := h.service.Create(r.Context(), &input, session.UserID)
	if err != nil {
		if errors.Is(err, ErrInvalidInput) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, "failed to create server", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)

	json.NewEncoder(w).Encode(server)
}

func (h *ServerHandler) GetServerForMemberHandler(w http.ResponseWriter, r *http.Request) {
	session, ok := auth.SessionFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	serverID, err := parseIDFromRequest(r)
	if err != nil {
		http.Error(w, "invalid server ID", http.StatusBadRequest)
		return
	}

	server, err := h.service.GetByIDForMember(r.Context(), serverID, session.UserID)
	if err != nil {
		if errors.Is(err, ErrInvalidInput) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if errors.Is(err, ErrServerNotFound) {
			http.Error(w, "server not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to get server", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	json.NewEncoder(w).Encode(server)
}

func (h *ServerHandler) ListForMemberHandler(w http.ResponseWriter, r *http.Request) {
	session, ok := auth.SessionFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	servers, err := h.service.ListForMember(r.Context(), session.UserID)
	if err != nil {
		if errors.Is(err, ErrInvalidInput) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, "failed to get servers", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	json.NewEncoder(w).Encode(servers)
}

func (h *ServerHandler) UpdateServerNameHandler(w http.ResponseWriter, r *http.Request) {
	session, ok := auth.SessionFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	serverID, err := parseIDFromRequest(r)
	if err != nil {
		http.Error(w, "invalid server ID", http.StatusBadRequest)
		return
	}

	var input UpdateServerNameInput
	err = json.NewDecoder(r.Body).Decode(&input)
	if err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	server, err := h.service.UpdateName(r.Context(), serverID, session.UserID, &input)
	if err != nil {
		if errors.Is(err, ErrInvalidInput) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if errors.Is(err, ErrForbidden) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		http.Error(w, "failed to update server name", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	json.NewEncoder(w).Encode(server)
}

func (h *ServerHandler) DeleteServerHandler(w http.ResponseWriter, r *http.Request) {
	session, ok := auth.SessionFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	serverID, err := parseIDFromRequest(r)
	if err != nil {
		http.Error(w, "invalid server ID", http.StatusBadRequest)
		return
	}

	err = h.service.DeleteServer(r.Context(), serverID, session.UserID)
	if err != nil {
		if errors.Is(err, ErrInvalidInput) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if errors.Is(err, ErrForbidden) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		http.Error(w, "failed to delete server", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
