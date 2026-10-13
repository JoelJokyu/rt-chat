package realtime

import (
	"context"
	"encoding/json"
	"log"
	"rtchat/internal/messages"
	"sync"

	"github.com/coder/websocket"
)

type Client struct {
	conn      *websocket.Conn
	userID    int64
	send      chan []byte
	done      chan struct{}
	closeOnce sync.Once
}

func NewClient(conn *websocket.Conn, userID int64) *Client {
	return &Client{
		conn:   conn,
		userID: userID,
		send:   make(chan []byte, 16),
		done:   make(chan struct{}),
	}
}

func (c *Client) Send(message []byte) bool {
	payload := append([]byte(nil), message...)

	select {
	case <-c.done:
		return false
	default:
	}

	select {
	case <-c.done:
		return false
	case c.send <- payload:
		return true
	default:
		return false
	}
}

func (c *Client) Close() {
	c.closeOnce.Do(func() {
		close(c.done)
		_ = c.conn.CloseNow()
	})
}

func (c *Client) ReadLoop(hub *Hub, channelID int64, messageService *messages.MessageService) {
	defer func() {
		hub.Unregister(channelID, c)
		c.Close()
	}()

	ctx := context.Background()

	for {
		_, content, err := c.conn.Read(ctx)
		if err != nil {
			return
		}

		savedMessage, err := messageService.Create(ctx, channelID, c.userID, &messages.CreateMessageInput{Content: string(content)})
		if err != nil {
			log.Printf("save websocket message: %v", err)
			continue
		}

		event := messages.MessageEvent{
			Type:      "message.created",
			ChannelID: channelID,
			Message:   savedMessage,
		}

		payload, err := json.Marshal(event)
		if err != nil {
			log.Printf("encode websocket message: %v", err)
			continue
		}

		hub.Broadcast(channelID, payload)
	}
}

func (c *Client) WriteLoop() {
	ctx := context.Background()

	for {
		select {
		case <-c.done:
			return
		case message := <-c.send:
			if err := c.conn.Write(ctx, websocket.MessageText, message); err != nil {
				c.Close()
				return
			}
		}
	}
}
