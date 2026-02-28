package protocol

import (
	"encoding/json"
	"errors"
	"fmt"
)

var ErrEmptyMessage = errors.New("empty message")

// Encode creates a binary message: [type_byte][payload].
// For data types (Stdout/Stdin) payload is raw bytes.
// For control types payload is JSON-encoded.
func Encode(msgType byte, payload any) ([]byte, error) {
	switch msgType {
	case TypeStdout, TypeStdin:
		data, ok := payload.([]byte)
		if !ok {
			return nil, fmt.Errorf("expected []byte for type 0x%02x", msgType)
		}
		msg := make([]byte, 1+len(data))
		msg[0] = msgType
		copy(msg[1:], data)
		return msg, nil
	case TypePing, TypePong, TypeEnd:
		return []byte{msgType}, nil
	default:
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("marshal payload: %w", err)
		}
		msg := make([]byte, 1+len(data))
		msg[0] = msgType
		copy(msg[1:], data)
		return msg, nil
	}
}

// Decode splits a binary message into type byte and raw payload.
func Decode(msg []byte) (byte, []byte, error) {
	if len(msg) == 0 {
		return 0, nil, ErrEmptyMessage
	}
	return msg[0], msg[1:], nil
}

// DecodeJSON decodes the JSON payload into the given struct.
func DecodeJSON(payload []byte, v any) error {
	return json.Unmarshal(payload, v)
}
