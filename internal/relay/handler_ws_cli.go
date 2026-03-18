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

const (
	relayPingInterval = 30 * time.Second
	relayPingTimeout  = 15 * time.Second
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
	ownerProvider, ownerSub, _, err := s.verifyToken(ctx, hello.Token)
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
		sessionID, _ = gonanoid.New(12)
		token := generateToken()

		// Determine session owner — delegated or direct
		sessionOwnerProvider := ownerProvider
		sessionOwnerSub := ownerSub
		serviceIdentity := ""
		if hello.DelegateFor != "" {
			serviceIdentity = ownerProvider + ":" + ownerSub
			sessionOwnerProvider = "delegated"
			sessionOwnerSub = hello.DelegateFor
		}

		info := SessionInfo{
			ID:              sessionID,
			OwnerProvider:   sessionOwnerProvider,
			OwnerSub:        sessionOwnerSub,
			Mode:            hello.Mode,
			Cols:            hello.Cols,
			Rows:            hello.Rows,
			Command:         hello.Command,
			Hostname:        hello.Hostname,
			ReconnectToken:  token,
			Lazy:            hello.Lazy,
			ProcessRunning:  !hello.Lazy,
			DelegateFor:     hello.DelegateFor,
			ServiceIdentity: serviceIdentity,
			Encrypted:       hello.Encrypted,
			EncryptionSalt:  hello.EncryptionSalt,
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

	defer s.hub.Disconnect(ctx, sessionID, s.gracePeriod)

	// Start server-side keepalive pings to detect dead CLI connections
	// and keep WebSocket alive through NAT/firewalls.
	connCtx, connCancel := context.WithCancel(ctx)
	defer connCancel()

	go func() {
		ticker := time.NewTicker(relayPingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-connCtx.Done():
				return
			case <-ticker.C:
				pingData, err := protocol.Encode(protocol.TypePing, nil)
				if err != nil {
					continue
				}
				writeCtx, cancel := context.WithTimeout(connCtx, relayPingTimeout)
				err = conn.Write(writeCtx, websocket.MessageBinary, pingData)
				cancel()
				if err != nil {
					s.logger.Info("cli keepalive ping failed", "session", sessionID, "err", err)
					conn.CloseNow()
					return
				}
			}
		}
	}()

	// Initialize CLI dimensions on the local session for resize priority tracking.
	if ls, ok := s.hub.GetLocal(sessionID); ok {
		ls.SetCLIDims(hello.Cols, hello.Rows)
	}

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
		case protocol.TypeStdin:
			// Local keyboard input notification from CLI — update resize priority.
			if ls, ok := s.hub.GetLocal(sessionID); ok {
				prev := ls.GetLastInputSource()
				ls.SetLastInputSource("cli")
				if prev == "viewer" {
					// Switching from viewer→CLI priority: restore CLI dimensions.
					cc, cr := ls.GetCLIDims()
					if cc > 0 && cr > 0 {
						s.hub.store.UpdateDimensions(ctx, sessionID, cc, cr)
						correction := protocol.Resize{Cols: cc, Rows: cr}
						encoded, _ := protocol.Encode(protocol.TypeResize, correction)
						ls.SendToCLI(ctx, encoded)
						s.hub.BroadcastOutput(ctx, sessionID, encoded)
					}
				}
			}
		case protocol.TypeResize:
			var sz protocol.Resize
			if err := protocol.DecodeJSON(payload, &sz); err == nil {
				if ls, ok := s.hub.GetLocal(sessionID); ok {
					ls.SetCLIDims(sz.Cols, sz.Rows)
					if ls.GetLastInputSource() == "viewer" {
						// Viewer has priority — send back viewer dims to correct CLI PTY.
						vc, vr := ls.GetViewerDims()
						if vc > 0 && vr > 0 {
							correction := protocol.Resize{Cols: vc, Rows: vr}
							encoded, _ := protocol.Encode(protocol.TypeResize, correction)
							ls.SendToCLI(ctx, encoded)
						}
					} else {
						// CLI has priority — update authoritative dims, broadcast to viewers.
						s.hub.store.UpdateDimensions(ctx, sessionID, sz.Cols, sz.Rows)
						encoded, err := protocol.Encode(protocol.TypeResize, sz)
						if err == nil {
							s.hub.BroadcastOutput(ctx, sessionID, encoded)
						}
					}
				}
			}
		case protocol.TypeProcessExited:
			s.hub.store.SetProcessExited(ctx, sessionID, true)
			s.hub.store.SetProcessRunning(ctx, sessionID, false)
			encoded, err := protocol.Encode(protocol.TypeProcessExited, payload)
			if err == nil {
				s.hub.BroadcastOutput(ctx, sessionID, encoded)
			}
			s.logger.Info("process exited", "session", sessionID)
		case protocol.TypeSpawnComplete:
			var sc protocol.SpawnComplete
			if err := protocol.DecodeJSON(payload, &sc); err == nil {
				s.hub.store.UpdateDimensions(ctx, sessionID, sc.Cols, sc.Rows)
				s.hub.store.SetProcessRunning(ctx, sessionID, true)
				s.hub.store.SetProcessExited(ctx, sessionID, false)
				s.logger.Info("spawn complete", "session", sessionID, "cols", sc.Cols, "rows", sc.Rows)
			}
		case protocol.TypeFileAck:
			// Route FileAck to the viewer that owns the transfer
			var ack protocol.FileAck
			if err := protocol.DecodeJSON(payload, &ack); err != nil {
				s.logger.Warn("failed to decode FileAck", "session", sessionID, "err", err)
				break
			}
			msg := make([]byte, 1+len(payload))
			msg[0] = protocol.TypeFileAck
			copy(msg[1:], payload)
			ls, ok := s.hub.GetLocal(sessionID)
			if !ok {
				s.logger.Warn("no local session for FileAck", "session", sessionID)
				break
			}
			ls.SendFileAck(ctx, ack.ID, msg)
			if ack.Status == "complete" || ack.Status == "error" {
				ls.CleanupFileTransfer(ack.ID)
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
