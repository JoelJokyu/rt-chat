package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"log"

	"github.com/redis/go-redis/v9"
)

const messageEventsTopic = "rtchat:message-events"

type RedisEventBus struct {
	client *redis.Client
}

func NewRedisEventBus(client *redis.Client) *RedisEventBus {
	return &RedisEventBus{client: client}
}

func (b *RedisEventBus) Publish(ctx context.Context, payload []byte) error {
	return b.client.Publish(ctx, messageEventsTopic, payload).Err()
}

func (b *RedisEventBus) Subscribe(ctx context.Context, hub *Hub) error {
	pubsub := b.client.Subscribe(ctx, messageEventsTopic)

	_, err := pubsub.Receive(ctx)
	if err != nil {
		_ = pubsub.Close()
		return err
	}
	defer pubsub.Close()
	log.Printf("Subscribed to Redis topic %q", messageEventsTopic)

	messages := pubsub.Channel()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case redisMessage, ok := <-messages:
			if !ok {
				if err := ctx.Err(); err != nil {
					return err
				}
				return errors.New("Redis Pub/Sub message channel closed unexpectedly")
			}

			var event struct {
				ChannelID int64 `json:"channel_id"`
			}

			if err := json.Unmarshal([]byte(redisMessage.Payload), &event); err != nil {
				log.Printf("decode Redis message event: %v", err)
				continue
			}

			hub.Broadcast(event.ChannelID, []byte(redisMessage.Payload))
		}
	}
}
