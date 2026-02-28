package relay

import (
	"context"
	"net/http"

	"github.com/coder/websocket"
	gonanoid "github.com/matoous/go-nanoid/v2"
	"github.com/brporter/phosphor/internal/protocol"
)

// HandleCLIWebSocket handles WebSocket connections from the CLI.
func (s *Server) HandleCLIWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		Subprotocols: []string{"phosphor"},
	})
	if err != nil {
		s.logger.Error("accept cli ws", "err", err)
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(1 << 20) // 1MB

	ctx := r.Context()

	// Read Hello message
	_, data, err := conn.Read(ctx)
	if err != nil {
		s.logger.Error("read hello", "err", err)
		return
	}

	msgType, payload, err := protocol.Decode(data)
	if err != nil || msgType != protocol.TypeHello {
		sendError(ctx, conn, "invalid_message", "expected Hello message")
		return
	}

	var hello protocol.Hello
	if err := protocol.DecodeJSON(payload, &hello); err != nil {
		sendError(ctx, conn, "invalid_payload", "invalid Hello payload")
		return
	}

	// Verify token and extract identity
	ownerProvider, ownerSub, err := s.verifyToken(ctx, hello.Token)
	if err != nil {
		sendError(ctx, conn, "auth_failed", "authentication failed: "+err.Error())
		return
	}

	// Create session
	sessionID, _ := gonanoid.New(12)
	session := NewSession(sessionID, ownerProvider, ownerSub, conn, hello, s.logger)
	s.hub.Register(session)
	defer s.hub.Unregister(sessionID)

	// Send Welcome
	viewURL := s.baseURL + "/session/" + sessionID
	welcome := protocol.Welcome{SessionID: sessionID, ViewURL: viewURL}
	welcomeData, _ := protocol.Encode(protocol.TypeWelcome, welcome)
	conn.Write(ctx, websocket.MessageBinary, welcomeData)

	// Read loop: forward CLI output to viewers
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			s.logger.Info("cli disconnected", "session", sessionID, "err", err)
			return
		}

		msgType, payload, err := protocol.Decode(data)
		if err != nil {
			continue
		}

		switch msgType {
		case protocol.TypeStdout:
			session.BroadcastToViewers(ctx, protocol.TypeStdout, payload)
		case protocol.TypeResize:
			var sz protocol.Resize
			if err := protocol.DecodeJSON(payload, &sz); err == nil {
				session.Cols = sz.Cols
				session.Rows = sz.Rows
				session.BroadcastToViewers(ctx, protocol.TypeResize, sz)
			}
		case protocol.TypePong:
			// heartbeat response, ignore
		}
	}
}

func sendError(ctx context.Context, conn *websocket.Conn, code, message string) {
	data, _ := protocol.Encode(protocol.TypeError, protocol.Error{Code: code, Message: message})
	conn.Write(ctx, websocket.MessageBinary, data)
	conn.Close(websocket.StatusPolicyViolation, message)
}
