// Package store provides Postgres-backed persistence for tenants, users,
// machines, and API keys.
package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	// ErrNotFound is returned when a requested row does not exist.
	ErrNotFound = errors.New("not found")
	// ErrDuplicateName is returned when a machine name is already taken
	// within a tenant.
	ErrDuplicateName = errors.New("machine name already in use")
	// ErrDuplicateFingerprint is returned when a machine public key is
	// already enrolled.
	ErrDuplicateFingerprint = errors.New("machine key already enrolled")
)

type Tenant struct {
	ID        uuid.UUID
	Name      string
	CreatedAt time.Time
}

type User struct {
	ID        uuid.UUID
	TenantID  uuid.UUID
	Provider  string
	Subject   string
	Email     string
	CreatedAt time.Time
}

type Machine struct {
	ID          uuid.UUID
	TenantID    uuid.UUID
	Name        string
	Hostname    string
	Fingerprint string
	CreatedAt   time.Time
	LastSeenAt  *time.Time
}

// Store wraps a pgx connection pool.
type Store struct {
	pool *pgxpool.Pool
}

// New connects to Postgres, applies migrations, and returns a Store.
func New(ctx context.Context, databaseURL string) (*Store, error) {
	if err := migrateUp(databaseURL); err != nil {
		return nil, fmt.Errorf("applying migrations: %w", err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("creating pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pinging database: %w", err)
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() {
	s.pool.Close()
}

// GetOrCreateUser looks up a user by federated identity, creating the user
// and a personal tenant on first login. A non-empty email updates a stale
// stored email.
func (s *Store) GetOrCreateUser(ctx context.Context, provider, subject, email string) (*User, error) {
	u, err := s.getUser(ctx, provider, subject)
	if err == nil {
		if email != "" && u.Email != email {
			_, err = s.pool.Exec(ctx, `UPDATE users SET email = $1 WHERE id = $2`, email, u.ID)
			if err != nil {
				return nil, fmt.Errorf("updating email: %w", err)
			}
			u.Email = email
		}
		return u, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, err
	}

	tenantName := email
	if tenantName == "" {
		tenantName = provider + ":" + subject
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("beginning tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var tenantID uuid.UUID
	err = tx.QueryRow(ctx,
		`INSERT INTO tenants (name) VALUES ($1) RETURNING id`, tenantName,
	).Scan(&tenantID)
	if err != nil {
		return nil, fmt.Errorf("creating tenant: %w", err)
	}

	u = &User{TenantID: tenantID, Provider: provider, Subject: subject, Email: email}
	err = tx.QueryRow(ctx,
		`INSERT INTO users (tenant_id, provider, subject, email)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (provider, subject) DO NOTHING
		 RETURNING id, created_at`,
		tenantID, provider, subject, email,
	).Scan(&u.ID, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		// Lost a create race: roll back the orphan tenant, use the winner's row.
		tx.Rollback(ctx)
		return s.getUser(ctx, provider, subject)
	}
	if err != nil {
		return nil, fmt.Errorf("creating user: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("committing: %w", err)
	}
	return u, nil
}

func (s *Store) getUser(ctx context.Context, provider, subject string) (*User, error) {
	u := &User{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, tenant_id, provider, subject, email, created_at
		 FROM users WHERE provider = $1 AND subject = $2`,
		provider, subject,
	).Scan(&u.ID, &u.TenantID, &u.Provider, &u.Subject, &u.Email, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return u, nil
}

// GetUserByID returns the user with the given ID.
func (s *Store) GetUserByID(ctx context.Context, id uuid.UUID) (*User, error) {
	u := &User{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, tenant_id, provider, subject, email, created_at
		 FROM users WHERE id = $1`, id,
	).Scan(&u.ID, &u.TenantID, &u.Provider, &u.Subject, &u.Email, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return u, nil
}

// CreateMachine enrolls a machine under a tenant.
func (s *Store) CreateMachine(ctx context.Context, tenantID uuid.UUID, name, hostname, fingerprint string) (*Machine, error) {
	m := &Machine{TenantID: tenantID, Name: name, Hostname: hostname, Fingerprint: fingerprint}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO machines (tenant_id, name, hostname, fingerprint)
		 VALUES ($1, $2, $3, $4) RETURNING id, created_at`,
		tenantID, name, hostname, fingerprint,
	).Scan(&m.ID, &m.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			if pgErr.ConstraintName == "machines_fingerprint_key" {
				return nil, ErrDuplicateFingerprint
			}
			return nil, ErrDuplicateName
		}
		return nil, fmt.Errorf("creating machine: %w", err)
	}
	return m, nil
}

const machineCols = `id, tenant_id, name, hostname, fingerprint, created_at, last_seen_at`

func scanMachine(row pgx.Row) (*Machine, error) {
	m := &Machine{}
	err := row.Scan(&m.ID, &m.TenantID, &m.Name, &m.Hostname, &m.Fingerprint, &m.CreatedAt, &m.LastSeenAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return m, nil
}

// GetMachine returns the machine with the given ID.
func (s *Store) GetMachine(ctx context.Context, id uuid.UUID) (*Machine, error) {
	return scanMachine(s.pool.QueryRow(ctx,
		`SELECT `+machineCols+` FROM machines WHERE id = $1`, id))
}

// GetMachineByFingerprint returns the machine whose enrolled public key has
// the given SHA256 fingerprint.
func (s *Store) GetMachineByFingerprint(ctx context.Context, fingerprint string) (*Machine, error) {
	return scanMachine(s.pool.QueryRow(ctx,
		`SELECT `+machineCols+` FROM machines WHERE fingerprint = $1`, fingerprint))
}

// ListMachines returns all machines in a tenant, ordered by name.
func (s *Store) ListMachines(ctx context.Context, tenantID uuid.UUID) ([]*Machine, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+machineCols+` FROM machines WHERE tenant_id = $1 ORDER BY name`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var machines []*Machine
	for rows.Next() {
		m, err := scanMachine(rows)
		if err != nil {
			return nil, err
		}
		machines = append(machines, m)
	}
	return machines, rows.Err()
}

// TouchMachine updates a machine's last_seen_at to now.
func (s *Store) TouchMachine(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `UPDATE machines SET last_seen_at = now() WHERE id = $1`, id)
	return err
}

// RenameMachine changes a machine's display name.
func (s *Store) RenameMachine(ctx context.Context, id uuid.UUID, name string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE machines SET name = $1 WHERE id = $2`, name, id)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrDuplicateName
		}
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteMachine removes a machine.
func (s *Store) DeleteMachine(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM machines WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// RecordAPIKey stores a newly generated API key ID for a user.
func (s *Store) RecordAPIKey(ctx context.Context, keyID string, userID uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO api_keys (key_id, user_id) VALUES ($1, $2)`, keyID, userID)
	return err
}

// IsAPIKeyRevoked reports whether a key ID is revoked. Unknown key IDs are
// treated as revoked so that keys minted before the database existed (or with
// a different signing secret) cannot be used.
func (s *Store) IsAPIKeyRevoked(ctx context.Context, keyID string) (bool, error) {
	var revokedAt *time.Time
	err := s.pool.QueryRow(ctx,
		`SELECT revoked_at FROM api_keys WHERE key_id = $1`, keyID,
	).Scan(&revokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return revokedAt != nil, nil
}

// RevokeAPIKey marks a key as revoked. It is scoped to the owning user so
// one user cannot revoke another's keys.
func (s *Store) RevokeAPIKey(ctx context.Context, keyID string, userID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE api_keys SET revoked_at = now() WHERE key_id = $1 AND user_id = $2 AND revoked_at IS NULL`,
		keyID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
