package relay

import (
	"context"
	"crypto/subtle"
	"net/http"
	"time"

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

	var session *Session
	var sessionID string

	if hello.SessionID != "" && hello.ReconnectToken != "" {
		// Reconnect path
		sessionID = hello.SessionID
		existing, ok := s.hub.Get(sessionID)
		if !ok {
			sendError(ctx, conn, "session_not_found", "session does not exist or has expired")
			return
		}
		if existing.OwnerProvider != ownerProvider || existing.OwnerSub != ownerSub {
			sendError(ctx, conn, "auth_failed", "session belongs to a different user")
			return
		}
		if subtle.ConstantTimeCompare([]byte(existing.ReconnectToken), []byte(hello.ReconnectToken)) != 1 {
			sendError(ctx, conn, "invalid_token", "invalid reconnect token")
			return
		}
		if !s.hub.Reconnect(sessionID, conn) {
			sendError(ctx, conn, "reconnect_failed", "session is not in a disconnected state")
			return
		}
		session = existing
		session.RotateReconnectToken()

		welcome := protocol.Welcome{
			SessionID:      sessionID,
			ViewURL:        s.baseURL + "/session/" + sessionID,
			ReconnectToken: session.ReconnectToken,
		}
		welcomeData, _ := protocol.Encode(protocol.TypeWelcome, welcome)
		conn.Write(ctx, websocket.MessageBinary, welcomeData)

		s.logger.Info("cli reconnected", "session", sessionID)
	} else {
		// New connection path
		sessionID, _ = gonanoid.New(12)
		session = NewSession(sessionID, ownerProvider, ownerSub, conn, hello, s.logger)
		s.hub.Register(session)

		welcome := protocol.Welcome{
			SessionID:      sessionID,
			ViewURL:        s.baseURL + "/session/" + sessionID,
			ReconnectToken: session.ReconnectToken,
		}
		welcomeData, _ := protocol.Encode(protocol.TypeWelcome, welcome)
		conn.Write(ctx, websocket.MessageBinary, welcomeData)
	}

	defer s.hub.Disconnect(sessionID, 60*time.Second)

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
