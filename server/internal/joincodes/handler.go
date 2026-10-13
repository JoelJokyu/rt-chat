package joincodes

import (
	"encoding/json"
	"errors"
	"net/http"
	"rtchat/internal/auth"
	"strconv"
)

type JoinCodeHandler struct {
	service *JoinCodeService
}

func NewJoinCodeHandler(service *JoinCodeService) *JoinCodeHandler {
	return &JoinCodeHandler{
		service: service,
	}
}

func (h *JoinCodeHandler) CreateJoinCodeHandler(w http.ResponseWriter, r *http.Request) {
	session, ok := auth.SessionFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var input CreateJoinCodeInput

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

	joinCode, rawToken, err := h.service.Create(r.Context(), serverID, session.UserID, &input)
	if err != nil {
		if errors.Is(err, ErrInvalidInput) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if errors.Is(err, ErrForbidden) {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		http.Error(w, "failed to create/save joincode", http.StatusInternalServerError)
		return
	}

	type createJoinCodeResponse struct {
		JoinCode *JoinCode `json:"join_code"`
		Code     string    `json:"code"`
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)

	json.NewEncoder(w).Encode(createJoinCodeResponse{
		JoinCode: joinCode,
		Code:     rawToken,
	})
}

func (h *JoinCodeHandler) JoinServerViaCodeHandler(w http.ResponseWriter, r *http.Request) {
	session, ok := auth.SessionFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var input JoinServerInput

	err := json.NewDecoder(r.Body).Decode(&input)
	if err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	serverID, alreadyMember, err := h.service.Join(r.Context(), session.UserID, &input)
	if err != nil {
		if errors.Is(err, ErrInvalidInput) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, "failed to join server via code", http.StatusInternalServerError)
		return
	}

	type joinServerViaCodeResponse struct {
		ServerID      int64 `json:"server_id"`
		AlreadyMember bool  `json:"already_member"`
	}

	status := http.StatusCreated
	if alreadyMember {
		status = http.StatusOK
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	json.NewEncoder(w).Encode(joinServerViaCodeResponse{
		ServerID:      serverID,
		AlreadyMember: alreadyMember,
	})
}

func (h *JoinCodeHandler) RevokeJoinCodeHandler(w http.ResponseWriter, r *http.Request) {
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

	codeID, err := strconv.ParseInt(r.PathValue("codeID"), 10, 64)
	if err != nil {
		http.Error(w, "invalid code ID", http.StatusBadRequest)
		return
	}

	err = h.service.RevokeCode(r.Context(), serverID, codeID, session.UserID)
	if err != nil {
		if errors.Is(err, ErrInvalidInput) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if errors.Is(err, ErrForbidden) {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		http.Error(w, "failed to revoke join code", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNoContent)
}
