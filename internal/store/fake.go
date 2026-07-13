package store

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Fake is an in-memory implementation of the store used in tests.
type Fake struct {
	mu       sync.Mutex
	tenants  map[uuid.UUID]*Tenant
	users    map[uuid.UUID]*User
	machines map[uuid.UUID]*Machine
	apiKeys  map[string]*fakeAPIKey
}

type fakeAPIKey struct {
	userID    uuid.UUID
	revokedAt *time.Time
}

// NewFake creates an empty in-memory store.
func NewFake() *Fake {
	return &Fake{
		tenants:  make(map[uuid.UUID]*Tenant),
		users:    make(map[uuid.UUID]*User),
		machines: make(map[uuid.UUID]*Machine),
		apiKeys:  make(map[string]*fakeAPIKey),
	}
}

func (f *Fake) GetOrCreateUser(_ context.Context, provider, subject, email string) (*User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, u := range f.users {
		if u.Provider == provider && u.Subject == subject {
			if email != "" && u.Email != email {
				u.Email = email
			}
			c := *u
			return &c, nil
		}
	}
	tenantName := email
	if tenantName == "" {
		tenantName = provider + ":" + subject
	}
	tenant := &Tenant{ID: uuid.New(), Name: tenantName, CreatedAt: time.Now()}
	f.tenants[tenant.ID] = tenant
	u := &User{ID: uuid.New(), TenantID: tenant.ID, Provider: provider, Subject: subject, Email: email, CreatedAt: time.Now()}
	f.users[u.ID] = u
	c := *u
	return &c, nil
}

func (f *Fake) GetUserByID(_ context.Context, id uuid.UUID) (*User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.users[id]
	if !ok {
		return nil, ErrNotFound
	}
	c := *u
	return &c, nil
}

func (f *Fake) CreateMachine(_ context.Context, tenantID uuid.UUID, name, hostname, fingerprint string) (*Machine, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, m := range f.machines {
		if m.Fingerprint == fingerprint {
			return nil, ErrDuplicateFingerprint
		}
		if m.TenantID == tenantID && m.Name == name {
			return nil, ErrDuplicateName
		}
	}
	m := &Machine{ID: uuid.New(), TenantID: tenantID, Name: name, Hostname: hostname, Fingerprint: fingerprint, CreatedAt: time.Now()}
	f.machines[m.ID] = m
	c := *m
	return &c, nil
}

func (f *Fake) GetMachine(_ context.Context, id uuid.UUID) (*Machine, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.machines[id]
	if !ok {
		return nil, ErrNotFound
	}
	c := *m
	return &c, nil
}

func (f *Fake) GetMachineByFingerprint(_ context.Context, fingerprint string) (*Machine, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, m := range f.machines {
		if m.Fingerprint == fingerprint {
			c := *m
			return &c, nil
		}
	}
	return nil, ErrNotFound
}

func (f *Fake) ListMachines(_ context.Context, tenantID uuid.UUID) ([]*Machine, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*Machine
	for _, m := range f.machines {
		if m.TenantID == tenantID {
			c := *m
			out = append(out, &c)
		}
	}
	return out, nil
}

func (f *Fake) TouchMachine(_ context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.machines[id]
	if !ok {
		return ErrNotFound
	}
	now := time.Now()
	m.LastSeenAt = &now
	return nil
}

func (f *Fake) RenameMachine(_ context.Context, id uuid.UUID, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.machines[id]
	if !ok {
		return ErrNotFound
	}
	for _, other := range f.machines {
		if other.ID != id && other.TenantID == m.TenantID && other.Name == name {
			return ErrDuplicateName
		}
	}
	m.Name = name
	return nil
}

func (f *Fake) DeleteMachine(_ context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.machines[id]; !ok {
		return ErrNotFound
	}
	delete(f.machines, id)
	return nil
}

func (f *Fake) RecordAPIKey(_ context.Context, keyID string, userID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.apiKeys[keyID] = &fakeAPIKey{userID: userID}
	return nil
}

func (f *Fake) IsAPIKeyRevoked(_ context.Context, keyID string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k, ok := f.apiKeys[keyID]
	if !ok {
		return true, nil
	}
	return k.revokedAt != nil, nil
}

func (f *Fake) RevokeAPIKey(_ context.Context, keyID string, userID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	k, ok := f.apiKeys[keyID]
	if !ok || k.userID != userID || k.revokedAt != nil {
		return ErrNotFound
	}
	now := time.Now()
	k.revokedAt = &now
	return nil
}
