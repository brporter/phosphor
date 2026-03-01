package protocol

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// TestEncode_Stdout verifies that encoding TypeStdout with []byte("hello")
// produces a message where the first byte is 0x01 followed by the raw payload.
func TestEncode_Stdout(t *testing.T) {
	input := []byte("hello")
	got, err := Encode(TypeStdout, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []byte{0x01, 'h', 'e', 'l', 'l', 'o'}
	if len(got) != len(want) {
		t.Fatalf("length mismatch: got %d, want %d", len(got), len(want))
	}
	for i, b := range want {
		if got[i] != b {
			t.Errorf("byte[%d]: got 0x%02x, want 0x%02x", i, got[i], b)
		}
	}
}

// TestEncode_Stdin verifies that encoding TypeStdin with []byte("input")
// produces a message where the first byte is 0x02 followed by the raw payload.
func TestEncode_Stdin(t *testing.T) {
	input := []byte("input")
	got, err := Encode(TypeStdin, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("expected non-empty message")
	}
	if got[0] != TypeStdin {
		t.Errorf("type byte: got 0x%02x, want 0x%02x", got[0], TypeStdin)
	}
	payload := got[1:]
	if string(payload) != "input" {
		t.Errorf("payload: got %q, want %q", string(payload), "input")
	}
}

// TestEncode_DataType_RejectsNonBytes verifies that passing a non-[]byte value
// for TypeStdout returns an error containing "expected []byte".
func TestEncode_DataType_RejectsNonBytes(t *testing.T) {
	_, err := Encode(TypeStdout, "not bytes")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "expected []byte") {
		t.Errorf("error %q does not contain %q", err.Error(), "expected []byte")
	}
}

// TestEncode_PingPongEnd verifies that TypePing, TypePong, and TypeEnd each
// encode to a single-byte message containing only the type byte.
func TestEncode_PingPongEnd(t *testing.T) {
	cases := []struct {
		name    string
		msgType byte
	}{
		{"TypePing", TypePing},
		{"TypePong", TypePong},
		{"TypeEnd", TypeEnd},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Encode(tc.msgType, nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != 1 {
				t.Fatalf("length: got %d, want 1", len(got))
			}
			if got[0] != tc.msgType {
				t.Errorf("type byte: got 0x%02x, want 0x%02x", got[0], tc.msgType)
			}
		})
	}
}

// TestEncode_JSONTypes verifies that each JSON-encoded message type produces
// a message of [type_byte | json_payload] and that the JSON round-trips correctly.
func TestEncode_JSONTypes(t *testing.T) {
	cases := []struct {
		name    string
		msgType byte
		payload any
	}{
		{
			name:    "TypeHello",
			msgType: TypeHello,
			payload: Hello{Token: "tok", Mode: "pty", Cols: 80, Rows: 24, Command: "bash"},
		},
		{
			name:    "TypeWelcome",
			msgType: TypeWelcome,
			payload: Welcome{SessionID: "ses1", ViewURL: "http://example.com/view/ses1"},
		},
		{
			name:    "TypeJoin",
			msgType: TypeJoin,
			payload: Join{Token: "tok", SessionID: "ses1"},
		},
		{
			name:    "TypeJoined",
			msgType: TypeJoined,
			payload: Joined{Mode: "pty", Cols: 120, Rows: 40, Command: "zsh"},
		},
		{
			name:    "TypeResize",
			msgType: TypeResize,
			payload: Resize{Cols: 100, Rows: 30},
		},
		{
			name:    "TypeError",
			msgType: TypeError,
			payload: Error{Code: "ERR_AUTH", Message: "unauthorized"},
		},
		{
			name:    "TypeViewerCount",
			msgType: TypeViewerCount,
			payload: ViewerCount{Count: 5},
		},
		{
			name:    "TypeMode",
			msgType: TypeMode,
			payload: Mode{Mode: "pipe"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Encode(tc.msgType, tc.payload)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) == 0 {
				t.Fatal("expected non-empty message")
			}
			if got[0] != tc.msgType {
				t.Errorf("type byte: got 0x%02x, want 0x%02x", got[0], tc.msgType)
			}
			// Verify the JSON payload is valid and matches the original struct
			jsonPayload := got[1:]
			if !json.Valid(jsonPayload) {
				t.Errorf("payload is not valid JSON: %s", string(jsonPayload))
			}
			// Re-encode the original payload and compare JSON bytes
			wantJSON, err := json.Marshal(tc.payload)
			if err != nil {
				t.Fatalf("could not marshal expected payload: %v", err)
			}
			if string(jsonPayload) != string(wantJSON) {
				t.Errorf("JSON payload mismatch:\n  got  %s\n  want %s", string(jsonPayload), string(wantJSON))
			}
		})
	}
}

// TestEncode_JSONMarshalError verifies that passing an unmarshalable value
// (such as a func) for a JSON type returns an error.
func TestEncode_JSONMarshalError(t *testing.T) {
	_, err := Encode(TypeHello, func() {})
	if err == nil {
		t.Fatal("expected error for unmarshalable payload, got nil")
	}
}

// TestDecode_ValidMessage verifies that decoding [0x01, 'h', 'e', 'l', 'l', 'o']
// returns type=0x01 and payload="hello".
func TestDecode_ValidMessage(t *testing.T) {
	msg := []byte{0x01, 'h', 'e', 'l', 'l', 'o'}
	msgType, payload, err := Decode(msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msgType != 0x01 {
		t.Errorf("type: got 0x%02x, want 0x01", msgType)
	}
	if string(payload) != "hello" {
		t.Errorf("payload: got %q, want %q", string(payload), "hello")
	}
}

// TestDecode_SingleByte verifies that decoding a single-byte message returns
// the correct type and an empty payload.
func TestDecode_SingleByte(t *testing.T) {
	msg := []byte{TypePing}
	msgType, payload, err := Decode(msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msgType != TypePing {
		t.Errorf("type: got 0x%02x, want 0x%02x", msgType, TypePing)
	}
	if len(payload) != 0 {
		t.Errorf("payload length: got %d, want 0", len(payload))
	}
}

// TestDecode_Empty verifies that decoding an empty slice returns ErrEmptyMessage.
func TestDecode_Empty(t *testing.T) {
	_, _, err := Decode([]byte{})
	if err == nil {
		t.Fatal("expected ErrEmptyMessage, got nil")
	}
	if !errors.Is(err, ErrEmptyMessage) {
		t.Errorf("error: got %v, want %v", err, ErrEmptyMessage)
	}
}

// TestDecodeJSON_Hello verifies that a JSON payload decodes into a Hello struct
// with all fields set correctly.
func TestDecodeJSON_Hello(t *testing.T) {
	payload := []byte(`{"token":"mytoken","mode":"pty","cols":80,"rows":24,"command":"bash"}`)
	var h Hello
	if err := DecodeJSON(payload, &h); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h.Token != "mytoken" {
		t.Errorf("Token: got %q, want %q", h.Token, "mytoken")
	}
	if h.Mode != "pty" {
		t.Errorf("Mode: got %q, want %q", h.Mode, "pty")
	}
	if h.Cols != 80 {
		t.Errorf("Cols: got %d, want 80", h.Cols)
	}
	if h.Rows != 24 {
		t.Errorf("Rows: got %d, want 24", h.Rows)
	}
	if h.Command != "bash" {
		t.Errorf("Command: got %q, want %q", h.Command, "bash")
	}
}

// TestDecodeJSON_InvalidJSON verifies that passing invalid JSON to DecodeJSON
// returns an error.
func TestDecodeJSON_InvalidJSON(t *testing.T) {
	payload := []byte(`{invalid json}`)
	var h Hello
	if err := DecodeJSON(payload, &h); err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

// TestEncodeDecode_RoundTrip verifies that encoding a message and then
// decoding it produces the original type byte and payload for all message types.
func TestEncodeDecode_RoundTrip(t *testing.T) {
	cases := []struct {
		name    string
		msgType byte
		payload any
		// For data types, rawPayload holds the expected decoded bytes.
		// For JSON types, we compare the JSON re-encoding.
		isData bool
	}{
		{
			name:    "TypeStdout",
			msgType: TypeStdout,
			payload: []byte("terminal output data"),
			isData:  true,
		},
		{
			name:    "TypeStdin",
			msgType: TypeStdin,
			payload: []byte("user input"),
			isData:  true,
		},
		{
			name:    "TypePing",
			msgType: TypePing,
			payload: nil,
			isData:  false,
		},
		{
			name:    "TypePong",
			msgType: TypePong,
			payload: nil,
			isData:  false,
		},
		{
			name:    "TypeEnd",
			msgType: TypeEnd,
			payload: nil,
			isData:  false,
		},
		{
			name:    "TypeHello",
			msgType: TypeHello,
			payload: Hello{Token: "t", Mode: "pty", Cols: 80, Rows: 24, Command: "sh"},
			isData:  false,
		},
		{
			name:    "TypeWelcome",
			msgType: TypeWelcome,
			payload: Welcome{SessionID: "abc", ViewURL: "http://host/view/abc"},
			isData:  false,
		},
		{
			name:    "TypeJoin",
			msgType: TypeJoin,
			payload: Join{Token: "t", SessionID: "abc"},
			isData:  false,
		},
		{
			name:    "TypeJoined",
			msgType: TypeJoined,
			payload: Joined{Mode: "pty", Cols: 80, Rows: 24, Command: "sh"},
			isData:  false,
		},
		{
			name:    "TypeResize",
			msgType: TypeResize,
			payload: Resize{Cols: 120, Rows: 40},
			isData:  false,
		},
		{
			name:    "TypeError",
			msgType: TypeError,
			payload: Error{Code: "ERR_NOTFOUND", Message: "session not found"},
			isData:  false,
		},
		{
			name:    "TypeViewerCount",
			msgType: TypeViewerCount,
			payload: ViewerCount{Count: 3},
			isData:  false,
		},
		{
			name:    "TypeMode",
			msgType: TypeMode,
			payload: Mode{Mode: "pipe"},
			isData:  false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			encoded, err := Encode(tc.msgType, tc.payload)
			if err != nil {
				t.Fatalf("Encode error: %v", err)
			}

			decodedType, decodedPayload, err := Decode(encoded)
			if err != nil {
				t.Fatalf("Decode error: %v", err)
			}

			if decodedType != tc.msgType {
				t.Errorf("type byte: got 0x%02x, want 0x%02x", decodedType, tc.msgType)
			}

			switch {
			case tc.msgType == TypePing || tc.msgType == TypePong || tc.msgType == TypeEnd:
				// Single-byte messages: payload must be empty
				if len(decodedPayload) != 0 {
					t.Errorf("payload length: got %d, want 0", len(decodedPayload))
				}
			case tc.isData:
				// Raw byte payload must match the original
				original := tc.payload.([]byte)
				if string(decodedPayload) != string(original) {
					t.Errorf("payload: got %q, want %q", string(decodedPayload), string(original))
				}
			default:
				// JSON payload: re-encode original and compare
				wantJSON, err := json.Marshal(tc.payload)
				if err != nil {
					t.Fatalf("could not marshal expected payload: %v", err)
				}
				if string(decodedPayload) != string(wantJSON) {
					t.Errorf("JSON payload mismatch:\n  got  %s\n  want %s",
						string(decodedPayload), string(wantJSON))
				}
			}
		})
	}
}
