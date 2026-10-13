package memberships

import (
	"encoding/json"
	"errors"
	"net/http"
	"rtchat/internal/auth"
	"strconv"
	"time"
)

type MembershipHandler struct {
	service *MembershipService
}

func NewMembershipHandler(service *MembershipService) *MembershipHandler {
	return &MembershipHandler{
		service: service,
	}
}

type UpdateMemberRoleInput struct {
	Role string `json:"role"`
}

func (h *MembershipHandler) ListMembersHandler(w http.ResponseWriter, r *http.Request) {
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

	limit := 10 //default

	rawLimit := r.URL.Query().Get("limit")
	if rawLimit != "" {
		parsedLimit, err := strconv.Atoi(rawLimit)
		if err != nil {
			http.Error(w, "invalid limit", http.StatusBadRequest)
			return
		}
		limit = parsedLimit
	}

	var before *MemberCursor

	rawJoinedAt := r.URL.Query().Get("before_joined_at")
	rawUserID := r.URL.Query().Get("before_user_id")

	if (rawJoinedAt == "") != (rawUserID == "") {
		http.Error(w, "cursor requires timestamp and ID", http.StatusBadRequest)
		return
	}

	if rawJoinedAt != "" && rawUserID != "" {
		joinedAt, err := time.Parse(time.RFC3339Nano, rawJoinedAt)
		if err != nil {
			http.Error(w, "invalid cursor timestamp", http.StatusBadRequest)
			return
		}

		userId, err := strconv.ParseInt(rawUserID, 10, 64)
		if err != nil {
			http.Error(w, "invalid cursor ID", http.StatusBadRequest)
			return
		}

		before = &MemberCursor{JoinedAt: joinedAt, UserID: userId}
	}

	members, hasMore, err := h.service.ListMembers(r.Context(), serverID, session.UserID, limit, before)
	if err != nil {
		if errors.Is(err, ErrInvalidInput) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if errors.Is(err, ErrForbidden) {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		http.Error(w, "failed to fetch members", http.StatusInternalServerError)
		return
	}

	var nextCursor *MemberCursor

	if hasMore && len(members) > 0 {
		last := members[len(members)-1]
		nextCursor = &MemberCursor{
			JoinedAt: last.JoinedAt,
			UserID:   last.UserID,
		}
	}

	type memberPageResponse struct {
		Members    []Member      `json:"members"`
		HasMore    bool          `json:"has_more"`
		NextCursor *MemberCursor `json:"next_cursor,omitempty"`
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	json.NewEncoder(w).Encode(memberPageResponse{
		Members:    members,
		HasMore:    hasMore,
		NextCursor: nextCursor,
	})
}

func (h *MembershipHandler) UpdateMemberRoleHandler(w http.ResponseWriter, r *http.Request) {
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

	targetUserID, err := strconv.ParseInt(r.PathValue("userID"), 10, 64)
	if err != nil {
		http.Error(w, "invalid user ID", http.StatusBadRequest)
		return
	}

	var input UpdateMemberRoleInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	updaterole, updatedOK, err := h.service.UpdateMemberRole(r.Context(), serverID, session.UserID, targetUserID, input.Role)
	if err != nil {
		if errors.Is(err, ErrInvalidInput) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if errors.Is(err, ErrForbidden) {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		http.Error(w, "failed to update member role", http.StatusInternalServerError)
		return
	}
	if !updatedOK {
		http.Error(w, "failed to update member role", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	json.NewEncoder(w).Encode(updaterole)
}

func (h *MembershipHandler) RemoveMemberHandler(w http.ResponseWriter, r *http.Request) {
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

	targetUserID, err := strconv.ParseInt(r.PathValue("userID"), 10, 64)
	if err != nil {
		http.Error(w, "invalid user ID", http.StatusBadRequest)
		return
	}

	updatedOK, err := h.service.RemoveMember(r.Context(), serverID, session.UserID, targetUserID)
	if err != nil {
		if errors.Is(err, ErrInvalidInput) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if errors.Is(err, ErrForbidden) {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		http.Error(w, "failed to remove member", http.StatusInternalServerError)
		return
	}
	if !updatedOK {
		http.Error(w, "failed to remove member", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	json.NewEncoder(w).Encode(map[string]bool{"updated": true})
}
