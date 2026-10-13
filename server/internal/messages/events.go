package messages

type MessageBroadcaster interface {
	Broadcast(channelID int64, payload []byte)
}

type MessageEvent struct {
	Type      string   `json:"type"`
	ChannelID int64    `json:"channel_id"`
	Message   *Message `json:"message,omitempty"`
	MessageID int64    `json:"message_id,omitempty"`
}
