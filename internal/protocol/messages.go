package protocol

// Message type bytes.
const (
	TypeStdout      byte = 0x01
	TypeStdin       byte = 0x02
	TypeResize      byte = 0x03
	TypeHello       byte = 0x10
	TypeWelcome     byte = 0x11
	TypeJoin        byte = 0x12
	TypeJoined      byte = 0x13
	TypeEnd         byte = 0x15
	TypeError       byte = 0x16
	TypeViewerCount byte = 0x20
	TypeMode        byte = 0x21
	TypePing        byte = 0x30
	TypePong        byte = 0x31
)

// Hello is sent by the CLI when connecting.
type Hello struct {
	Token   string `json:"token"`
	Mode    string `json:"mode"` // "pty" or "pipe"
	Cols    int    `json:"cols"`
	Rows    int    `json:"rows"`
	Command string `json:"command"`
}

// Welcome is sent by the server in response to Hello.
type Welcome struct {
	SessionID string `json:"session_id"`
	ViewURL   string `json:"view_url"`
}

// Join is sent by a viewer to attach to a session.
type Join struct {
	Token     string `json:"token"`
	SessionID string `json:"session_id"`
}

// Joined is sent to the viewer after successful join.
type Joined struct {
	Mode    string `json:"mode"`
	Cols    int    `json:"cols"`
	Rows    int    `json:"rows"`
	Command string `json:"command"`
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
