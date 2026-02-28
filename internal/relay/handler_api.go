package relay

import (
	"encoding/json"
	"net/http"
)

type sessionInfo struct {
	ID      string `json:"id"`
	Mode    string `json:"mode"`
	Cols    int    `json:"cols"`
	Rows    int    `json:"rows"`
	Command string `json:"command"`
	Viewers int    `json:"viewers"`
}

// HandleListSessions returns the sessions owned by the authenticated user.
func (s *Server) HandleListSessions(w http.ResponseWriter, r *http.Request) {
	provider, sub, err := s.extractIdentity(r)
	if err != nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	sessions := s.hub.ListForOwner(provider, sub)
	infos := make([]sessionInfo, 0, len(sessions))
	for _, sess := range sessions {
		infos = append(infos, sessionInfo{
			ID:      sess.ID,
			Mode:    sess.Mode,
			Cols:    sess.Cols,
			Rows:    sess.Rows,
			Command: sess.Command,
			Viewers: sess.ViewerCount(),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(infos)
}
