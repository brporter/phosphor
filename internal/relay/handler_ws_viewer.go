package relay

import (
	"net/http"

	"github.com/coder/websocket"
	gonanoid "github.com/matoous/go-nanoid/v2"
	"github.com/brporter/phosphor/internal/protocol"
)

// HandleViewerWebSocket handles WebSocket connections from browser viewers.
func (s *Server) HandleViewerWebSocket(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		Subprotocols: []string{"phosphor"},
	})
	if err != nil {
		s.logger.Error("accept viewer ws", "err", err)
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(64 << 10) // 64KB for viewer messages

	ctx := r.Context()

	// Read Join message
	_, data, err := conn.Read(ctx)
	if err != nil {
		s.logger.Error("read join", "err", err)
		return
	}

	msgType, payload, err := protocol.Decode(data)
	if err != nil || msgType != protocol.TypeJoin {
		sendError(ctx, conn, "invalid_message", "expected Join message")
		return
	}

	var join protocol.Join
	if err := protocol.DecodeJSON(payload, &join); err != nil {
		sendError(ctx, conn, "invalid_payload", "invalid Join payload")
		return
	}

	// Override session ID from URL path
	join.SessionID = sessionID

	// Verify token and extract identity
	viewerProvider, viewerSub, viewerEmail, err := s.verifyToken(ctx, join.Token)
	if err != nil {
		sendError(ctx, conn, "auth_failed", "authentication failed: "+err.Error())
		return
	}

	// Look up session metadata
	info, ok, err := s.hub.Get(ctx, join.SessionID)
	if err != nil {
		sendError(ctx, conn, "internal_error", "failed to look up session")
		return
	}
	if !ok {
		sendError(ctx, conn, "session_not_found", "session not found")
		return
	}

	// Verify ownership: viewer must be the session owner
	isOwner := viewerProvider == info.OwnerProvider && viewerSub == info.OwnerSub
	// For delegated sessions, match viewer by sub or email
	if !isOwner && info.DelegateFor != "" {
		isOwner = viewerSub == info.DelegateFor ||
			(viewerEmail != "" && viewerEmail == info.DelegateFor)
	}
	if !isOwner {
		sendError(ctx, conn, "forbidden", "you do not own this session")
		return
	}

	// Get or create local session for this viewer
	ls, err := s.hub.GetOrCreateViewerLocal(ctx, join.SessionID)
	if err != nil {
		sendError(ctx, conn, "internal_error", "failed to set up viewer session")
		return
	}

	// Add viewer
	viewerID, _ := gonanoid.New(8)
	if !ls.AddViewer(viewerID, conn) {
		sendError(ctx, conn, "session_full", "maximum viewers reached")
		return
	}
	defer func() {
		ls.RemoveViewer(viewerID)
		ls.NotifyViewerCount(ctx)
		s.hub.CleanupViewerLocal(join.SessionID)
	}()

	// Send Joined
	joined := protocol.Joined{
		Mode:    info.Mode,
		Cols:    info.Cols,
		Rows:    info.Rows,
		Command: info.Command,
	}
	joinedData, _ := protocol.Encode(protocol.TypeJoined, joined)
	conn.Write(ctx, websocket.MessageBinary, joinedData)

	// Replay scrollback buffer so the viewer sees prior output
	if buf := ls.GetScrollback(); len(buf) > 0 {
		scrollbackMsg, _ := protocol.Encode(protocol.TypeStdout, buf)
		conn.Write(ctx, websocket.MessageBinary, scrollbackMsg)
	}

	// Notify CLI of new viewer count
	ls.NotifyViewerCount(ctx)

	// For lazy sessions that haven't spawned yet, send SpawnRequest to CLI
	if info.Lazy && !info.ProcessRunning && !info.ProcessExited {
		spawnData, _ := protocol.Encode(protocol.TypeSpawnRequest, nil)
		s.hub.SendInput(ctx, join.SessionID, spawnData)
		s.logger.Info("sent spawn request for lazy session", "session", sessionID)
	} else if info.ProcessExited {
		// If process has exited, trigger a restart
		s.hub.RestartProcess(ctx, join.SessionID)
		s.logger.Info("viewer triggered process restart", "session", sessionID)
	}

	s.logger.Info("viewer joined", "session", sessionID, "viewer", viewerID)

	// Read loop: forward viewer input to CLI
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			s.logger.Info("viewer disconnected", "session", sessionID, "viewer", viewerID)
			return
		}

		msgType, payload, err := protocol.Decode(data)
		if err != nil {
			continue
		}

		switch msgType {
		case protocol.TypeStdin:
			if info.Mode == "pty" {
				encoded, err := protocol.Encode(protocol.TypeStdin, payload)
				if err == nil {
					s.hub.SendInput(ctx, join.SessionID, encoded)
				}
			}
			// In pipe mode, stdin from viewers is silently dropped
		case protocol.TypeResize:
			var sz protocol.Resize
			if err := protocol.DecodeJSON(payload, &sz); err == nil {
				encoded, err := protocol.Encode(protocol.TypeResize, sz)
				if err == nil {
					s.hub.SendInput(ctx, join.SessionID, encoded)
				}
			}
		case protocol.TypeRestart:
			s.hub.RestartProcess(ctx, join.SessionID)
			s.logger.Info("viewer requested process restart", "session", sessionID, "viewer", viewerID)
		case protocol.TypePong:
			// heartbeat response
		}
	}
}
