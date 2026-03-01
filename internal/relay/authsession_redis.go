package relay

import (
	"context"
	"time"

	nanoid "github.com/matoous/go-nanoid/v2"
	"github.com/redis/go-redis/v9"
)

const redisAuthTTL = 5 * time.Minute

// RedisAuthSessionStore is a Redis-backed implementation of AuthSessionStoreI.
//
// Redis key schema:
//
//	auth:{id} → Hash with 5-minute TTL (PKCE state)
type RedisAuthSessionStore struct {
	rdb *redis.Client
}

// NewRedisAuthSessionStore creates a new Redis-backed auth session store.
func NewRedisAuthSessionStore(rdb *redis.Client) *RedisAuthSessionStore {
	return &RedisAuthSessionStore{rdb: rdb}
}

func authKey(id string) string { return "auth:" + id }

func (s *RedisAuthSessionStore) Create(ctx context.Context, provider, codeVerifier, source string) (AuthSessionData, error) {
	id, _ := nanoid.New()
	now := time.Now()

	pipe := s.rdb.Pipeline()
	pipe.HSet(ctx, authKey(id), map[string]interface{}{
		"id":            id,
		"provider":      provider,
		"code_verifier": codeVerifier,
		"source":        source,
		"created_at":    now.Format(time.RFC3339Nano),
		"id_token":      "",
	})
	pipe.Expire(ctx, authKey(id), redisAuthTTL)

	if _, err := pipe.Exec(ctx); err != nil {
		return AuthSessionData{}, err
	}

	return AuthSessionData{
		ID:           id,
		Provider:     provider,
		CodeVerifier: codeVerifier,
		Source:       source,
		CreatedAt:    now,
	}, nil
}

func (s *RedisAuthSessionStore) Get(ctx context.Context, id string) (AuthSessionData, bool, error) {
	vals, err := s.rdb.HGetAll(ctx, authKey(id)).Result()
	if err != nil {
		return AuthSessionData{}, false, err
	}
	if len(vals) == 0 {
		return AuthSessionData{}, false, nil
	}

	createdAt, _ := time.Parse(time.RFC3339Nano, vals["created_at"])

	return AuthSessionData{
		ID:           vals["id"],
		Provider:     vals["provider"],
		CodeVerifier: vals["code_verifier"],
		Source:       vals["source"],
		IDToken:      vals["id_token"],
		CreatedAt:    createdAt,
	}, true, nil
}

func (s *RedisAuthSessionStore) Complete(ctx context.Context, id, idToken string) error {
	return s.rdb.HSet(ctx, authKey(id), "id_token", idToken).Err()
}

func (s *RedisAuthSessionStore) Consume(ctx context.Context, id string) (string, bool, error) {
	// Use a transaction to atomically read and delete
	var token string
	err := s.rdb.Watch(ctx, func(tx *redis.Tx) error {
		idToken, err := tx.HGet(ctx, authKey(id), "id_token").Result()
		if err == redis.Nil || idToken == "" {
			return redis.Nil
		}
		if err != nil {
			return err
		}
		token = idToken

		_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.Del(ctx, authKey(id))
			return nil
		})
		return err
	}, authKey(id))

	if err == redis.Nil {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return token, true, nil
}

// Stop is a no-op for Redis — TTL handles cleanup automatically.
func (s *RedisAuthSessionStore) Stop() {}
