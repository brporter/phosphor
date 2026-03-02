package relay

import (
	"encoding/json"
	"net/http"
)

type sessionListItem struct {
	ID      string `json:"id"`
	Mode    string `json:"mode"`
	Cols    int    `json:"cols"`
	Rows    int    `json:"rows"`
	Command string `json:"command"`
	Viewers       int    `json:"viewers"`
	ProcessExited bool   `json:"process_exited"`
}

// HandleDestroySession terminates a session. Only the session owner can destroy it.
func (s *Server) HandleDestroySession(w http.ResponseWriter, r *http.Request) {
	provider, sub, err := s.extractIdentity(r)
	if err != nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	id := r.PathValue("id")
	info, ok, err := s.hub.Get(r.Context(), id)
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}

	if info.OwnerProvider != provider || info.OwnerSub != sub {
		http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
		return
	}

	s.hub.Unregister(r.Context(), id)
	w.WriteHeader(http.StatusNoContent)
}

// HandleListSessions returns the sessions owned by the authenticated user.
func (s *Server) HandleListSessions(w http.ResponseWriter, r *http.Request) {
	provider, sub, err := s.extractIdentity(r)
	if err != nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	sessions, err := s.hub.ListForOwner(r.Context(), provider, sub)
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	infos := make([]sessionListItem, 0, len(sessions))
	for _, info := range sessions {
		viewers := 0
		if ls, ok := s.hub.GetLocal(info.ID); ok {
			viewers = ls.ViewerCount()
		}
		infos = append(infos, sessionListItem{
			ID:      info.ID,
			Mode:    info.Mode,
			Cols:    info.Cols,
			Rows:    info.Rows,
			Command: info.Command,
			Viewers:       viewers,
			ProcessExited: info.ProcessExited,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(infos)
}
