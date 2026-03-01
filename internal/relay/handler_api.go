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
