package realtime

import (
	"log"
	"net/http"
	"rtchat/internal/auth"
	"rtchat/internal/memberships"
	"rtchat/internal/messages"
	"strconv"

	"github.com/coder/websocket"
)

type Handler struct {
	membershipService *memberships.MembershipService
	messageService    *messages.MessageService
	hub               *Hub
}

func NewHandler(membershipService *memberships.MembershipService, messageService *messages.MessageService, hub *Hub) *Handler {
	return &Handler{
		membershipService: membershipService,
		messageService:    messageService,
		hub:               hub,
	}
}

func (h *Handler) ChatHandler(w http.ResponseWriter, r *http.Request) {
	session, ok := auth.SessionFromContext(r.Context())
	if !ok {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}

	channelID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || channelID <= 0 {
		http.Error(w, "invalid channel ID", http.StatusBadRequest)
		return
	}

	isMember, err := h.membershipService.IsMemberOfChannel(r.Context(), channelID, session.UserID)
	if err != nil {
		log.Printf("check channel membership: %v", err)
		http.Error(w, "could not check channel membership", http.StatusInternalServerError)
		return
	}
	if !isMember {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		log.Printf("websocket accept: %v", err)
		return
	}

	client := NewClient(conn, session.UserID)

	h.hub.Register(channelID, client)
	defer func() {
		h.hub.Unregister(channelID, client)
		client.Close()
	}()

	go client.WriteLoop()
	client.ReadLoop(h.hub, channelID, h.messageService)
}
