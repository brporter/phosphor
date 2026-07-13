package relay

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/ssh"

	"github.com/brporter/phosphor/internal/store"
)

// TunnelDialer is the sshgate surface the relay needs (implemented by
// *sshgate.Registry). It is nil until SetSSHGate is called.
type TunnelDialer interface {
	Online(machineID string) bool
	Dial(machineID string) (net.Conn, error)
	Close(machineID string) bool
}

// SetSSHGate wires the SSH gateway into the HTTP server: tunnel liveness for
// the machines API, byte piping for the WS bridge, and the advertised SSH
// endpoint + host key for /api/ssh-info.
func (s *Server) SetSSHGate(tunnels TunnelDialer, publicAddr string, hostKey ssh.PublicKey) {
	s.tunnels = tunnels
	s.sshPublicAddr = publicAddr
	s.sshHostKey = hostKey
}

// resolveUser authenticates the request and bootstraps the user + personal
// tenant on first login.
func (s *Server) resolveUser(r *http.Request) (*store.User, error) {
	provider, sub, email, err := s.extractIdentity(r)
	if err != nil {
		return nil, err
	}
	return s.db.GetOrCreateUser(r.Context(), provider, sub, email)
}

type machineJSON struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Hostname    string     `json:"hostname"`
	Fingerprint string     `json:"fingerprint"`
	Online      bool       `json:"online"`
	CreatedAt   time.Time  `json:"created_at"`
	LastSeenAt  *time.Time `json:"last_seen_at"`
}

func (s *Server) machineJSON(m *store.Machine) machineJSON {
	online := false
	if s.tunnels != nil {
		online = s.tunnels.Online(m.ID.String())
	}
	return machineJSON{
		ID:          m.ID.String(),
		Name:        m.Name,
		Hostname:    m.Hostname,
		Fingerprint: m.Fingerprint,
		Online:      online,
		CreatedAt:   m.CreatedAt,
		LastSeenAt:  m.LastSeenAt,
	}
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// HandleListMachines returns the tenant's machines.
// GET /api/machines
func (s *Server) HandleListMachines(w http.ResponseWriter, r *http.Request) {
	user, err := s.resolveUser(r)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	machines, err := s.db.ListMachines(r.Context(), user.TenantID)
	if err != nil {
		s.logger.Error("listing machines", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]machineJSON, 0, len(machines))
	for _, m := range machines {
		out = append(out, s.machineJSON(m))
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

// HandleCreateMachine enrolls a machine (public key in authorized_keys
// format) under the caller's tenant.
// POST /api/machines
func (s *Server) HandleCreateMachine(w http.ResponseWriter, r *http.Request) {
	user, err := s.resolveUser(r)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req struct {
		Name      string `json:"name"`
		Hostname  string `json:"hostname"`
		PublicKey string `json:"public_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		req.Name = strings.TrimSpace(req.Hostname)
	}
	if req.Name == "" {
		writeJSONError(w, http.StatusBadRequest, "name is required")
		return
	}
	pubKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(req.PublicKey))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "public_key must be in authorized_keys format")
		return
	}

	m, err := s.db.CreateMachine(r.Context(), user.TenantID, req.Name, req.Hostname, ssh.FingerprintSHA256(pubKey))
	switch {
	case errors.Is(err, store.ErrDuplicateName):
		writeJSONError(w, http.StatusConflict, "a machine with that name already exists")
		return
	case errors.Is(err, store.ErrDuplicateFingerprint):
		writeJSONError(w, http.StatusConflict, "that machine key is already enrolled")
		return
	case err != nil:
		s.logger.Error("creating machine", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	s.logger.Info("machine enrolled", "machine", m.ID, "tenant", user.TenantID, "name", m.Name)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(s.machineJSON(m))
}

// tenantMachine loads a machine and verifies it belongs to the caller's tenant.
func (s *Server) tenantMachine(w http.ResponseWriter, r *http.Request) (*store.User, *store.Machine, bool) {
	user, err := s.resolveUser(r)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return nil, nil, false
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "machine not found")
		return nil, nil, false
	}
	m, err := s.db.GetMachine(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) || (err == nil && m.TenantID != user.TenantID) {
		writeJSONError(w, http.StatusNotFound, "machine not found")
		return nil, nil, false
	}
	if err != nil {
		s.logger.Error("loading machine", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return nil, nil, false
	}
	return user, m, true
}

// HandleUpdateMachine renames a machine.
// PATCH /api/machines/{id}
func (s *Server) HandleUpdateMachine(w http.ResponseWriter, r *http.Request) {
	_, m, ok := s.tenantMachine(w, r)
	if !ok {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		writeJSONError(w, http.StatusBadRequest, "name is required")
		return
	}
	if err := s.db.RenameMachine(r.Context(), m.ID, strings.TrimSpace(req.Name)); err != nil {
		if errors.Is(err, store.ErrDuplicateName) {
			writeJSONError(w, http.StatusConflict, "a machine with that name already exists")
			return
		}
		s.logger.Error("renaming machine", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	m.Name = strings.TrimSpace(req.Name)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.machineJSON(m))
}

// HandleDeleteMachine unenrolls a machine and closes its tunnel.
// DELETE /api/machines/{id}
func (s *Server) HandleDeleteMachine(w http.ResponseWriter, r *http.Request) {
	_, m, ok := s.tenantMachine(w, r)
	if !ok {
		return
	}
	if err := s.db.DeleteMachine(r.Context(), m.ID); err != nil && !errors.Is(err, store.ErrNotFound) {
		s.logger.Error("deleting machine", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if s.tunnels != nil {
		s.tunnels.Close(m.ID.String())
	}
	w.WriteHeader(http.StatusNoContent)
}

// HandleSSHInfo advertises the SSH gateway endpoint and host key so CLIs can
// pin them over TLS at enrollment.
// GET /api/ssh-info
func (s *Server) HandleSSHInfo(w http.ResponseWriter, r *http.Request) {
	if s.sshHostKey == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "ssh gateway not configured")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"addr":        s.sshPublicAddr,
		"host_key":    strings.TrimSpace(string(ssh.MarshalAuthorizedKey(s.sshHostKey))),
		"fingerprint": ssh.FingerprintSHA256(s.sshHostKey),
	})
}
