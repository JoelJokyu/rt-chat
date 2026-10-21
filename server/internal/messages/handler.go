package messages

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"rtchat/internal/auth"
	"strconv"
	"time"
)

type MessageHandler struct {
	service   *MessageService
	publisher MessagePublisher
}

func NewMessageHandler(service *MessageService, publisher MessagePublisher) *MessageHandler {
	return &MessageHandler{
		service:   service,
		publisher: publisher,
	}
}

func (h *MessageHandler) CreateMessageHandler(w http.ResponseWriter, r *http.Request) {
	session, ok := auth.SessionFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var input CreateMessageInput

	channelID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid channel ID", http.StatusBadRequest)
		return
	}

	err = json.NewDecoder(r.Body).Decode(&input)
	if err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	message, err := h.service.Create(r.Context(), channelID, session.UserID, &input)
	if err != nil {
		if errors.Is(err, ErrInvalidInput) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if errors.Is(err, ErrForbidden) {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		http.Error(w, "failed to create message", http.StatusInternalServerError)
		return
	}

	h.publishEvent(r.Context(), MessageEvent{
		Type:      "message.created",
		ChannelID: message.ChannelID,
		Message:   message,
	})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)

	json.NewEncoder(w).Encode(message)
}

func (h *MessageHandler) ListMessagesHandler(w http.ResponseWriter, r *http.Request) {
	session, ok := auth.SessionFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	channelID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid channel ID", http.StatusBadRequest)
		return
	}

	limit := 50 //default

	rawLimit := r.URL.Query().Get("limit")
	if rawLimit != "" {
		parsedLimit, err := strconv.Atoi(rawLimit)
		if err != nil {
			http.Error(w, "invalid limit", http.StatusBadRequest)
			return
		}
		limit = parsedLimit
	}

	var before *MessageCursor

	rawCreatedAt := r.URL.Query().Get("before_created_at")
	rawID := r.URL.Query().Get("before_id")

	if (rawCreatedAt == "") != (rawID == "") {
		http.Error(w, "cursor requires timestamp and ID", http.StatusBadRequest)
		return
	}

	if rawCreatedAt != "" && rawID != "" {
		createdAt, err := time.Parse(time.RFC3339Nano, rawCreatedAt)
		if err != nil {
			http.Error(w, "invalid cursor timestamp", http.StatusBadRequest)
			return
		}

		id, err := strconv.ParseInt(rawID, 10, 64)
		if err != nil {
			http.Error(w, "invalid cursor ID", http.StatusBadRequest)
			return
		}

		before = &MessageCursor{CreatedAt: createdAt, ID: id}
	}

	messages, hasMore, err := h.service.ListMessages(r.Context(), channelID, session.UserID, limit, before)
	if err != nil {
		if errors.Is(err, ErrInvalidInput) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if errors.Is(err, ErrForbidden) {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		http.Error(w, "failed to fetch messages", http.StatusInternalServerError)
		return
	}

	var nextCursor *MessageCursor

	if hasMore && len(messages) > 0 {
		last := messages[len(messages)-1]
		nextCursor = &MessageCursor{
			CreatedAt: last.CreatedAt,
			ID:        last.ID,
		}
	}

	type messagePageResponse struct {
		Messages   []Message      `json:"messages"`
		HasMore    bool           `json:"has_more"`
		NextCursor *MessageCursor `json:"next_cursor,omitempty"`
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	json.NewEncoder(w).Encode(messagePageResponse{
		Messages:   messages,
		HasMore:    hasMore,
		NextCursor: nextCursor,
	})
}

func (h *MessageHandler) UpdateMessageHandler(w http.ResponseWriter, r *http.Request) {
	session, ok := auth.SessionFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	messageID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid message ID", http.StatusBadRequest)
		return
	}

	var input UpdateMessageInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	message, err := h.service.UpdateMessage(r.Context(), messageID, session.UserID, &input)
	if err != nil {
		if errors.Is(err, ErrInvalidInput) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if errors.Is(err, ErrForbidden) {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		http.Error(w, "failed to update message", http.StatusInternalServerError)
		return
	}

	h.publishEvent(r.Context(), MessageEvent{
		Type:      "message.updated",
		ChannelID: message.ChannelID,
		Message:   message,
	})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	json.NewEncoder(w).Encode(message)
}

func (h *MessageHandler) DeleteMessageHandler(w http.ResponseWriter, r *http.Request) {
	session, ok := auth.SessionFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	messageID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid message ID", http.StatusBadRequest)
		return
	}

	message, err := h.service.DeleteMessage(r.Context(), messageID, session.UserID)
	if err != nil {
		if errors.Is(err, ErrInvalidInput) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if errors.Is(err, ErrForbidden) {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		http.Error(w, "failed to delete message", http.StatusInternalServerError)
		return
	}

	h.publishEvent(r.Context(), MessageEvent{
		Type:      "message.deleted",
		ChannelID: message.ChannelID,
		MessageID: message.ID,
	})

	w.WriteHeader(http.StatusNoContent)
}

func (h *MessageHandler) publishEvent(ctx context.Context, event MessageEvent) {
	payload, err := json.Marshal(event)
	if err != nil {
		log.Printf("encode message event: %v", err)
		return
	}

	if err := h.publisher.Publish(ctx, payload); err != nil {
		// The message change is already committed in PostgreSQL. A Pub/Sub
		// failure means live clients may miss this notification until they fetch history.
		log.Printf("publish message event: %v", err)
	}
}
