package protocol

// Message type bytes.
// Mirrored in web/src/lib/protocol.ts and windows/Phosphor/Models/ProtocolMessages.cs — kept manually in sync.
const (
	TypeStdout      byte = 0x01
	TypeStdin       byte = 0x02
	TypeResize      byte = 0x03
	TypeHello       byte = 0x10
	TypeWelcome     byte = 0x11
	TypeJoin        byte = 0x12
	TypeJoined      byte = 0x13
	TypeReconnect   byte = 0x14
	TypeEnd         byte = 0x15
	TypeError         byte = 0x16
	TypeProcessExited byte = 0x17
	TypeRestart       byte = 0x18
	TypeViewerCount   byte = 0x20
	TypeMode          byte = 0x21
	TypeSpawnRequest  byte = 0x22
	TypeSpawnComplete byte = 0x23
	TypePing          byte = 0x30
	TypePong          byte = 0x31

	TypeFileStart byte = 0x40
	TypeFileChunk byte = 0x41
	TypeFileEnd   byte = 0x42
	TypeFileAck   byte = 0x43
)

// Hello is sent by the CLI when connecting.
type Hello struct {
	Token   string `json:"token"`
	Mode    string `json:"mode"` // "pty" or "pipe"
	Cols    int    `json:"cols"`
	Rows    int    `json:"rows"`
	Command  string `json:"command"`
	Hostname string `json:"hostname,omitempty"`

	Lazy        bool   `json:"lazy,omitempty"`
	DelegateFor string `json:"delegate_for,omitempty"`

	// End-to-end encryption
	Encrypted      bool   `json:"encrypted,omitempty"`
	EncryptionSalt string `json:"encryption_salt,omitempty"` // base64-encoded

	// Set on reconnect attempts
	SessionID      string `json:"session_id,omitempty"`
	ReconnectToken string `json:"reconnect_token,omitempty"`
}

// Welcome is sent by the server in response to Hello.
type Welcome struct {
	SessionID      string `json:"session_id"`
	ViewURL        string `json:"view_url"`
	ReconnectToken string `json:"reconnect_token"`
}

// Join is sent by a viewer to attach to a session.
type Join struct {
	Token     string `json:"token"`
	SessionID string `json:"session_id"`
}

// Joined is sent to the viewer after successful join.
type Joined struct {
	Mode           string `json:"mode"`
	Cols           int    `json:"cols"`
	Rows           int    `json:"rows"`
	Command        string `json:"command"`
	Encrypted      bool   `json:"encrypted,omitempty"`
	EncryptionSalt string `json:"encryption_salt,omitempty"`
}

// Resize carries terminal dimensions.
type Resize struct {
	Cols int `json:"cols"`
	Rows int `json:"rows"`
}

// Error carries an error from the server.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ViewerCount notifies the CLI how many viewers are connected.
type ViewerCount struct {
	Count int `json:"count"`
}

// Mode notifies viewers of the session mode.
type Mode struct {
	Mode string `json:"mode"`
}

// Reconnect notifies viewers of CLI disconnect/reconnect events.
type Reconnect struct {
	Status string `json:"status"` // "disconnected" or "reconnected"
}

// ProcessExited is sent by the CLI when the subprocess exits.
type ProcessExited struct {
	ExitCode int `json:"exit_code"`
}

// SpawnComplete is sent by the daemon to the relay after spawning a shell.
type SpawnComplete struct {
	Cols int `json:"cols"`
	Rows int `json:"rows"`
}

// FileStart initiates a file upload from viewer to CLI.
type FileStart struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// FileEnd signals the end of a file transfer with a hash for verification.
type FileEnd struct {
	ID     string `json:"id"`
	SHA256 string `json:"sha256"`
}

// FileAck is sent by the CLI to report file transfer progress/completion/error.
type FileAck struct {
	ID           string `json:"id"`
	Status       string `json:"status"` // accepted, progress, complete, error
	Error        string `json:"error,omitempty"`
	BytesWritten int64  `json:"bytes_written,omitempty"`
}
