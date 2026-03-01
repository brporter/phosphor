package relay

import (
	"sync"
	"time"

	nanoid "github.com/matoous/go-nanoid/v2"
)

// AuthSession represents a pending browser-based auth flow.
type AuthSession struct {
	ID           string
	Provider     string
	CodeVerifier string
	CreatedAt    time.Time
	IDToken      string // populated when the callback completes
}

// AuthSessionStore manages pending auth sessions with TTL.
type AuthSessionStore struct {
	mu       sync.Mutex
	sessions map[string]*AuthSession
	ttl      time.Duration
	stopCh   chan struct{}
}

// NewAuthSessionStore creates a store with background cleanup.
func NewAuthSessionStore(ttl time.Duration) *AuthSessionStore {
	s := &AuthSessionStore{
		sessions: make(map[string]*AuthSession),
		ttl:      ttl,
		stopCh:   make(chan struct{}),
	}
	go s.cleanup()
	return s
}

// Create starts a new pending auth session.
func (s *AuthSessionStore) Create(provider, codeVerifier string) *AuthSession {
	id, _ := nanoid.New()
	sess := &AuthSession{
		ID:           id,
		Provider:     provider,
		CodeVerifier: codeVerifier,
		CreatedAt:    time.Now(),
	}
	s.mu.Lock()
	s.sessions[id] = sess
	s.mu.Unlock()
	return sess
}

// Get returns a session if it exists and hasn't expired.
func (s *AuthSessionStore) Get(id string) (*AuthSession, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if !ok {
		return nil, false
	}
	if time.Since(sess.CreatedAt) > s.ttl {
		delete(s.sessions, id)
		return nil, false
	}
	return sess, true
}

// Complete sets the ID token on a session.
func (s *AuthSessionStore) Complete(id, idToken string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[id]; ok {
		sess.IDToken = idToken
	}
}

// Consume returns the ID token and deletes the session (single-use).
func (s *AuthSessionStore) Consume(id string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if !ok || sess.IDToken == "" {
		return "", false
	}
	token := sess.IDToken
	delete(s.sessions, id)
	return token, true
}

// Stop shuts down the cleanup goroutine.
func (s *AuthSessionStore) Stop() {
	close(s.stopCh)
}

func (s *AuthSessionStore) cleanup() {
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
