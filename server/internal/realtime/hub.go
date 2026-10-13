package realtime

import "sync"

type Hub struct {
	mu               sync.RWMutex
	clientsByChannel map[int64]map[*Client]struct{}
}

func NewHub() *Hub {
	return &Hub{
		clientsByChannel: make(map[int64]map[*Client]struct{}),
	}
}

func (h *Hub) Register(channelID int64, client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.clientsByChannel[channelID] == nil {
		h.clientsByChannel[channelID] = make(map[*Client]struct{})
	}

	h.clientsByChannel[channelID][client] = struct{}{}
}

func (h *Hub) Unregister(channelID int64, client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	clients := h.clientsByChannel[channelID]
	delete(clients, client)

	if len(clients) == 0 {
		delete(h.clientsByChannel, channelID)
	}
}

func (h *Hub) Broadcast(channelID int64, message []byte) {
	h.mu.RLock()

	clients := make([]*Client, 0, len(h.clientsByChannel[channelID]))
	for client := range h.clientsByChannel[channelID] {
		clients = append(clients, client)
	}

	h.mu.RUnlock()

	for _, client := range clients {
		if !client.Send(message) {
			h.Unregister(channelID, client)
			client.Close()
		}
	}
}
