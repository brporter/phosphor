package relay

import (
	"context"
	"sync"
	"time"

	nanoid "github.com/matoous/go-nanoid/v2"
)

// MemoryAuthSessionStore is an in-memory implementation of AuthSessionStoreI.
type MemoryAuthSessionStore struct {
	mu       sync.Mutex
	sessions map[string]AuthSessionData
	ttl      time.Duration
	stopCh   chan struct{}
}

// NewMemoryAuthSessionStore creates a store with background cleanup.
func NewMemoryAuthSessionStore(ttl time.Duration) *MemoryAuthSessionStore {
	s := &MemoryAuthSessionStore{
		sessions: make(map[string]AuthSessionData),
		ttl:      ttl,
		stopCh:   make(chan struct{}),
	}
	go s.cleanup()
	return s
}

func (s *MemoryAuthSessionStore) Create(_ context.Context, provider, codeVerifier, source string) (AuthSessionData, error) {
	id, _ := nanoid.New()
	sess := AuthSessionData{
		ID:           id,
		Provider:     provider,
		CodeVerifier: codeVerifier,
		Source:       source,
		CreatedAt:    time.Now(),
	}
	s.mu.Lock()
	s.sessions[id] = sess
	s.mu.Unlock()
	return sess, nil
}

func (s *MemoryAuthSessionStore) Get(_ context.Context, id string) (AuthSessionData, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if !ok {
		return AuthSessionData{}, false, nil
	}
	if time.Since(sess.CreatedAt) > s.ttl {
		delete(s.sessions, id)
		return AuthSessionData{}, false, nil
	}
	return sess, true, nil
}

func (s *MemoryAuthSessionStore) SetProvider(_ context.Context, id, provider, codeVerifier string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[id]; ok {
		sess.Provider = provider
		sess.CodeVerifier = codeVerifier
		s.sessions[id] = sess
	}
	return nil
}

func (s *MemoryAuthSessionStore) Complete(_ context.Context, id, idToken string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[id]; ok {
		sess.IDToken = idToken
		s.sessions[id] = sess
	}
	return nil
}

func (s *MemoryAuthSessionStore) Consume(_ context.Context, id string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if !ok || sess.IDToken == "" {
		return "", false, nil
	}
	token := sess.IDToken
	delete(s.sessions, id)
	return token, true, nil
}

func (s *MemoryAuthSessionStore) Stop() {
	close(s.stopCh)
}

func (s *MemoryAuthSessionStore) cleanup() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.mu.Lock()
			now := time.Now()
			for id, sess := range s.sessions {
				if now.Sub(sess.CreatedAt) > s.ttl {
					delete(s.sessions, id)
				}
			}
			s.mu.Unlock()
		}
	}
}
