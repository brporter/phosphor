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
	viewerProvider, viewerSub, err := s.verifyToken(ctx, join.Token)
	if err != nil {
		sendError(ctx, conn, "auth_failed", "authentication failed: "+err.Error())
		return
	}

	// Look up session
	session, ok := s.hub.Get(join.SessionID)
	if !ok {
		sendError(ctx, conn, "session_not_found", "session not found")
		return
	}

	// Verify ownership: viewer must be the session owner
	if viewerProvider != session.OwnerProvider || viewerSub != session.OwnerSub {
		sendError(ctx, conn, "forbidden", "you do not own this session")
		return
	}

	// Add viewer
	viewerID, _ := gonanoid.New(8)
	if !session.AddViewer(viewerID, conn) {
		sendError(ctx, conn, "session_full", "maximum viewers reached")
		return
	}
	defer func() {
		session.RemoveViewer(viewerID)
		session.NotifyViewerCount(ctx)
	}()

	// Send Joined
	joined := protocol.Joined{
		Mode:    session.Mode,
		Cols:    session.Cols,
		Rows:    session.Rows,
		Command: session.Command,
	}
	joinedData, _ := protocol.Encode(protocol.TypeJoined, joined)
	conn.Write(ctx, websocket.MessageBinary, joinedData)

	// Notify CLI of new viewer count
	session.NotifyViewerCount(ctx)

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
			if session.Mode == "pty" {
				session.SendToCLI(ctx, protocol.TypeStdin, payload)
			}
			// In pipe mode, stdin from viewers is silently dropped
		case protocol.TypeResize:
			session.SendToCLI(ctx, protocol.TypeResize, payload)
		case protocol.TypePong:
			// heartbeat response
		}
	}
}
