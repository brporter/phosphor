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

	var sessionID string

	if hello.SessionID != "" && hello.ReconnectToken != "" {
		// Reconnect path
		sessionID = hello.SessionID
		existing, ok, err := s.hub.Get(ctx, sessionID)
		if err != nil {
			sendError(ctx, conn, "internal_error", "failed to look up session")
			return
		}
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
		if !existing.Disconnected {
			sendError(ctx, conn, "reconnect_failed", "session is not in a disconnected state")
			return
		}

		newToken := generateToken()
		if err := s.hub.Reconnect(ctx, sessionID, conn, newToken); err != nil {
			sendError(ctx, conn, "reconnect_failed", "reconnect failed")
			return
		}

		welcome := protocol.Welcome{
			SessionID:      sessionID,
			ViewURL:        s.baseURL + "/session/" + sessionID,
			ReconnectToken: newToken,
		}
		welcomeData, _ := protocol.Encode(protocol.TypeWelcome, welcome)
		conn.Write(ctx, websocket.MessageBinary, welcomeData)

		s.logger.Info("cli reconnected", "session", sessionID)
	} else {
		// New connection path
		sessionID, _ = gonanoid.New(12)
		token := generateToken()
		info := SessionInfo{
			ID:             sessionID,
			OwnerProvider:  ownerProvider,
			OwnerSub:       ownerSub,
			Mode:           hello.Mode,
			Cols:           hello.Cols,
			Rows:           hello.Rows,
			Command:        hello.Command,
			ReconnectToken: token,
		}

		if _, err := s.hub.Register(ctx, info, conn); err != nil {
			sendError(ctx, conn, "internal_error", "failed to register session")
			return
		}

		welcome := protocol.Welcome{
			SessionID:      sessionID,
			ViewURL:        s.baseURL + "/session/" + sessionID,
			ReconnectToken: token,
		}
		welcomeData, _ := protocol.Encode(protocol.TypeWelcome, welcome)
		conn.Write(ctx, websocket.MessageBinary, welcomeData)
	}

	defer s.hub.Disconnect(ctx, sessionID, 60*time.Second)

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
			encoded, err := protocol.Encode(protocol.TypeStdout, payload)
			if err == nil {
				s.hub.BroadcastOutput(ctx, sessionID, encoded)
			}
			// Buffer raw stdout for viewer replay
			if ls, ok := s.hub.GetLocal(sessionID); ok {
				ls.AppendScrollback(payload)
			}
		case protocol.TypeResize:
			var sz protocol.Resize
			if err := protocol.DecodeJSON(payload, &sz); err == nil {
				s.hub.store.UpdateDimensions(ctx, sessionID, sz.Cols, sz.Rows)
				encoded, err := protocol.Encode(protocol.TypeResize, sz)
				if err == nil {
					s.hub.BroadcastOutput(ctx, sessionID, encoded)
				}
			}
		case protocol.TypeProcessExited:
			s.hub.store.SetProcessExited(ctx, sessionID, true)
			encoded, err := protocol.Encode(protocol.TypeProcessExited, payload)
			if err == nil {
				s.hub.BroadcastOutput(ctx, sessionID, encoded)
			}
			s.logger.Info("process exited", "session", sessionID)
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
