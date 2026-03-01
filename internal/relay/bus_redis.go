package relay

import (
	"context"
	"log/slog"

	"github.com/redis/go-redis/v9"
)

// RedisMessageBus wraps Redis Pub/Sub for cross-relay messaging.
type RedisMessageBus struct {
	rdb    *redis.Client
	logger *slog.Logger
}

// NewRedisMessageBus creates a new Redis-backed message bus.
func NewRedisMessageBus(rdb *redis.Client, logger *slog.Logger) *RedisMessageBus {
	return &RedisMessageBus{rdb: rdb, logger: logger}
}

func (b *RedisMessageBus) Publish(ctx context.Context, channel string, msg []byte) error {
	return b.rdb.Publish(ctx, channel, msg).Err()
}

func (b *RedisMessageBus) Subscribe(ctx context.Context, channel string) (<-chan []byte, func(), error) {
	pubsub := b.rdb.Subscribe(ctx, channel)

	// Wait for subscription confirmation
	_, err := pubsub.Receive(ctx)
	if err != nil {
		pubsub.Close()
		return nil, nil, err
	}

	ch := make(chan []byte, 64)
	redisCh := pubsub.Channel()

	go func() {
		defer close(ch)
		for msg := range redisCh {
			select {
			case ch <- []byte(msg.Payload):
			default:
				// drop on slow consumer
				b.logger.Debug("dropping message on slow consumer", "channel", channel)
			}
		}
	}()

	unsub := func() {
		pubsub.Close()
	}

	return ch, unsub, nil
}
