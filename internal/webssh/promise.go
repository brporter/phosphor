//go:build js && wasm

package webssh

import (
	"errors"
	"syscall/js"
)

// await blocks the calling goroutine until a JS promise settles, returning
// its resolved value or an error. It must never be called from within a JS
// callback (that would deadlock the single JS thread); SSH auth callbacks run
// on a Go goroutine, so this is safe there.
func await(promise js.Value) (js.Value, error) {
	if promise.Type() != js.TypeObject || promise.Get("then").Type() != js.TypeFunction {
		// Not a thenable — treat as an immediate value.
		return promise, nil
	}
	type result struct {
		val js.Value
		err error
	}
	ch := make(chan result, 1)

	var onResolve, onReject js.Func
	onResolve = js.FuncOf(func(this js.Value, args []js.Value) any {
		var v js.Value
		if len(args) > 0 {
			v = args[0]
		}
		ch <- result{val: v}
		onResolve.Release()
		onReject.Release()
		return nil
	})
	onReject = js.FuncOf(func(this js.Value, args []js.Value) any {
		msg := "rejected"
		if len(args) > 0 && args[0].Truthy() {
			if m := args[0].Get("message"); m.Type() == js.TypeString {
				msg = m.String()
			} else {
				msg = args[0].String()
			}
		}
		ch <- result{err: errors.New(msg)}
		onResolve.Release()
		onReject.Release()
		return nil
	})
	promise.Call("then", onResolve, onReject)

	r := <-ch
	return r.val, r.err
}
