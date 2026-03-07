package relay

import (
	"context"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisSessionStore is a Redis-backed implementation of SessionStore.
//
// Redis key schema:
//
//	session:{id}           → Hash (metadata fields)
//	owner:{provider}:{sub} → Set (session IDs)
//	expiry:{id}            → String with TTL (grace period marker)
type RedisSessionStore struct {
	rdb      *redis.Client
	logger   *slog.Logger
	mu       sync.Mutex
	onExpiry ExpiryCallback
	stopCh   chan struct{}
}

// NewRedisSessionStore creates a new Redis-backed session store.
func NewRedisSessionStore(rdb *redis.Client, logger *slog.Logger) *RedisSessionStore {
	return &RedisSessionStore{
		rdb:    rdb,
		logger: logger,
		stopCh: make(chan struct{}),
	}
}

// SetExpiryCallback sets the function called when a session's grace period expires.
func (s *RedisSessionStore) SetExpiryCallback(fn ExpiryCallback) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onExpiry = fn
}

func sessionKey(id string) string       { return "session:" + id }
func ownerKey(provider, sub string) string { return "owner:" + provider + ":" + sub }
func expiryKey(id string) string         { return "expiry:" + id }

func (s *RedisSessionStore) Register(ctx context.Context, info SessionInfo) error {
	pipe := s.rdb.Pipeline()

	pipe.HSet(ctx, sessionKey(info.ID), map[string]interface{}{
		"id":               info.ID,
		"owner_provider":   info.OwnerProvider,
		"owner_sub":        info.OwnerSub,
		"mode":             info.Mode,
		"command":          info.Command,
		"reconnect_token":  info.ReconnectToken,
		"relay_id":         info.RelayID,
		"cols":             info.Cols,
		"rows":             info.Rows,
		"disconnected":     "false",
		"disconnected_at":  "",
		"process_exited":   "false",
		"lazy":             strconv.FormatBool(info.Lazy),
		"process_running":  strconv.FormatBool(info.ProcessRunning),
		"delegate_for":     info.DelegateFor,
		"service_identity": info.ServiceIdentity,
	})

	pipe.SAdd(ctx, ownerKey(info.OwnerProvider, info.OwnerSub), info.ID)

	_, err := pipe.Exec(ctx)
	return err
}

func (s *RedisSessionStore) Unregister(ctx context.Context, sessionID string) error {
	// Get owner info to clean up the owner index
	info, ok, err := s.Get(ctx, sessionID)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	pipe := s.rdb.Pipeline()
	pipe.Del(ctx, sessionKey(sessionID))
	pipe.Del(ctx, expiryKey(sessionID))
	pipe.SRem(ctx, ownerKey(info.OwnerProvider, info.OwnerSub), sessionID)
	_, err = pipe.Exec(ctx)
	return err
}

func (s *RedisSessionStore) Get(ctx context.Context, sessionID string) (SessionInfo, bool, error) {
	vals, err := s.rdb.HGetAll(ctx, sessionKey(sessionID)).Result()
	if err != nil {
		return SessionInfo{}, false, err
	}
	if len(vals) == 0 {
		return SessionInfo{}, false, nil
	}

	cols, _ := strconv.Atoi(vals["cols"])
	rows, _ := strconv.Atoi(vals["rows"])
	disconnected := vals["disconnected"] == "true"
	processExited := vals["process_exited"] == "true"
	lazy := vals["lazy"] == "true"
	processRunning := vals["process_running"] == "true"

	var disconnectedAt time.Time
	if vals["disconnected_at"] != "" {
		disconnectedAt, _ = time.Parse(time.RFC3339Nano, vals["disconnected_at"])
	}

	return SessionInfo{
		ID:              vals["id"],
		OwnerProvider:   vals["owner_provider"],
		OwnerSub:        vals["owner_sub"],
		Mode:            vals["mode"],
		Command:         vals["command"],
		ReconnectToken:  vals["reconnect_token"],
		RelayID:         vals["relay_id"],
		Cols:            cols,
		Rows:            rows,
		Disconnected:    disconnected,
		DisconnectedAt:  disconnectedAt,
		ProcessExited:   processExited,
		Lazy:            lazy,
		ProcessRunning:  processRunning,
		DelegateFor:     vals["delegate_for"],
		ServiceIdentity: vals["service_identity"],
	}, true, nil
}

func (s *RedisSessionStore) ListForOwner(ctx context.Context, provider, sub string) ([]SessionInfo, error) {
	ids, err := s.rdb.SMembers(ctx, ownerKey(provider, sub)).Result()
	if err != nil {
		return nil, err
	}

	var result []SessionInfo
	for _, id := range ids {
		info, ok, err := s.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		if ok {
			result = append(result, info)
		} else {
			// Stale entry in owner set — clean up
			s.rdb.SRem(ctx, ownerKey(provider, sub), id)
		}
	}
	return result, nil
}

func (s *RedisSessionStore) SetDisconnected(ctx context.Context, sessionID string) error {
	now := time.Now().Format(time.RFC3339Nano)
	return s.rdb.HSet(ctx, sessionKey(sessionID),
		"disconnected", "true",
		"disconnected_at", now,
	).Err()
}

func (s *RedisSessionStore) SetReconnected(ctx context.Context, sessionID, newToken, relayID string) error {
	return s.rdb.HSet(ctx, sessionKey(sessionID),
		"disconnected", "false",
		"disconnected_at", "",
		"reconnect_token", newToken,
		"relay_id", relayID,
	).Err()
}

func (s *RedisSessionStore) UpdateDimensions(ctx context.Context, sessionID string, cols, rows int) error {
	return s.rdb.HSet(ctx, sessionKey(sessionID),
		"cols", cols,
		"rows", rows,
	).Err()
}

func (s *RedisSessionStore) ScheduleExpiry(ctx context.Context, sessionID string, grace time.Duration) error {
	return s.rdb.Set(ctx, expiryKey(sessionID), "1", grace).Err()
}

func (s *RedisSessionStore) CancelExpiry(ctx context.Context, sessionID string) error {
	return s.rdb.Del(ctx, expiryKey(sessionID)).Err()
}

func (s *RedisSessionStore) SetProcessExited(ctx context.Context, sessionID string, exited bool) error {
	val := "false"
	if exited {
		val = "true"
	}
	return s.rdb.HSet(ctx, sessionKey(sessionID), "process_exited", val).Err()
}

func (s *RedisSessionStore) SetProcessRunning(ctx context.Context, sessionID string, running bool) error {
	val := "false"
	if running {
		val = "true"
	}
	return s.rdb.HSet(ctx, sessionKey(sessionID), "process_running", val).Err()
}

// StartExpiryPoller periodically checks for disconnected sessions whose expiry key has vanished.
func (s *RedisSessionStore) StartExpiryPoller(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-s.stopCh:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.pollExpiries(ctx)
			}
		}
	}()
}

func (s *RedisSessionStore) pollExpiries(ctx context.Context) {
	var cursor uint64
	for {
		keys, next, err := s.rdb.Scan(ctx, cursor, "session:*", 100).Result()
		if err != nil {
			s.logger.Error("expiry poll scan failed", "err", err)
			return
		}

		for _, key := range keys {
			sessionID := key[len("session:"):]

			// Check if disconnected
			disc, err := s.rdb.HGet(ctx, key, "disconnected").Result()
			if err != nil || disc != "true" {
				continue
			}

			// Check if expiry key still exists
			exists, err := s.rdb.Exists(ctx, expiryKey(sessionID)).Result()
			if err != nil {
				continue
			}
			if exists == 0 {
				// Expiry key gone — grace period expired
				s.mu.Lock()
				cb := s.onExpiry
				s.mu.Unlock()
				if cb != nil {
					cb(ctx, sessionID)
				}
			}
		}

		cursor = next
		if cursor == 0 {
			break
		}
	}
}

// Stop shuts down the expiry poller.
func (s *RedisSessionStore) Stop() {
	close(s.stopCh)
}
