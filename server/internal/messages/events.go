package messages

import "context"

type MessagePublisher interface {
	Publish(ctx context.Context, payload []byte) error
}

type MessageEvent struct {
	Type      string   `json:"type"`
	ChannelID int64    `json:"channel_id"`
	Message   *Message `json:"message,omitempty"`
	MessageID int64    `json:"message_id,omitempty"`
}
