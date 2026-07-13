//go:build js && wasm

package webssh

import (
	"encoding/json"
	"errors"
	"syscall/js"
)

// openAndAuth waits for the WebSocket to open, sends the auth prelude, and
// waits for the relay's {"ok":true} acknowledgement — all before the SSH
// client takes over the socket. It temporarily installs its own listeners
// and removes them once the ack arrives.
func openAndAuth(ws js.Value, token string) error {
	type ev struct {
		kind string
		data string
	}
	ch := make(chan ev, 4)

	var onOpen, onMessage, onError, onClose js.Func
	release := func() {
		onOpen.Release()
		onMessage.Release()
		onError.Release()
		onClose.Release()
	}

	onOpen = js.FuncOf(func(this js.Value, args []js.Value) any {
		ch <- ev{kind: "open"}
		return nil
	})
	onMessage = js.FuncOf(func(this js.Value, args []js.Value) any {
		data := args[0].Get("data")
		if data.Type() == js.TypeString {
			ch <- ev{kind: "message", data: data.String()}
		}
		return nil
	})
	onError = js.FuncOf(func(this js.Value, args []js.Value) any {
		ch <- ev{kind: "error"}
		return nil
	})
	onClose = js.FuncOf(func(this js.Value, args []js.Value) any {
		ch <- ev{kind: "close"}
		return nil
	})

	ws.Call("addEventListener", "open", onOpen)
	ws.Call("addEventListener", "message", onMessage)
	ws.Call("addEventListener", "error", onError)
	ws.Call("addEventListener", "close", onClose)

	defer func() {
		ws.Call("removeEventListener", "open", onOpen)
		ws.Call("removeEventListener", "message", onMessage)
		ws.Call("removeEventListener", "error", onError)
		ws.Call("removeEventListener", "close", onClose)
		release()
	}()

	// If the socket is already open, send immediately.
	sendAuth := func() {
		payload, _ := json.Marshal(map[string]string{"token": token})
		ws.Call("send", string(payload))
	}
	if ws.Get("readyState").Int() == 1 { // OPEN
		sendAuth()
	}

	for e := range ch {
		switch e.kind {
		case "open":
			sendAuth()
		case "message":
			var ack struct {
				OK    bool   `json:"ok"`
				Error string `json:"error"`
			}
			if err := json.Unmarshal([]byte(e.data), &ack); err != nil {
				return errors.New("invalid auth ack")
			}
			if !ack.OK {
				if ack.Error != "" {
					return errors.New(ack.Error)
				}
				return errors.New("authentication rejected")
			}
			return nil
		case "error":
			return errors.New("websocket error before auth")
		case "close":
			return errors.New("websocket closed before auth")
		}
	}
	return errors.New("auth prelude aborted")
}
