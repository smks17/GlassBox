//go:build js && wasm

package main

import (
	"context"
	"fmt"
	"glass/internal/engine"
	"glass/internal/engine/user"
	"sync"
	"syscall/js"
)

// reply is the shape the browser reads every command through.
func reply(r engine.Result) map[string]any {
	return map[string]any{
		"stdout":   r.Stdout,
		"stderr":   r.Stderr,
		"exitCode": r.ExitCode,
		"cwd":      r.Cwd,
		"user":     r.User,
		"at":       r.At,
	}
}

// fail is the same shape for what never reached the engine.
func fail(app *engine.App, message string) map[string]any {
	return reply(engine.Result{Stderr: message, ExitCode: 2, Cwd: app.Cwd(), User: app.User(), At: app.Now()})
}

func main() {
	app := engine.NewApp()
	if err := app.Login(user.RootName, user.RootName); err != nil {
		js.Global().Get("console").Call("error", fmt.Sprintf("GlassBox login: %v", err))
		return
	}
	js.Global().Set("__engineReady", js.ValueOf(true))
	shutdown := sync.OnceValue(func() bool {
		return app.Close(context.Background()) == nil
	})
	// TODO(stream-io): the engine can already stream output and be cancelled,
	// but nothing here uses it yet. To connect it:
	//   - app.SetYield with a function that parks on a MessageChannel, so a
	//     loop that never blocks still lets the page deliver a keypress.
	//   - execute returns a Promise and takes an onChunk callback, calling
	//     app.ExecuteStream on a goroutine instead of the blocking app.Execute.
	//   - an interrupt() global that cancels the context ExecuteStream was given.
	//   - Terminal.tsx and wasm-bridge.ts change with it: they move together.
	js.Global().Set("execute", js.FuncOf(func(this js.Value, args []js.Value) (result any) {
		if len(args) < 1 || args[0].Type() != js.TypeString {
			return fail(app, "execute: expected a string")
		}
		defer func() {
			if r := recover(); r != nil {
				result = fail(app, fmt.Sprintf("internal error: %v", r))
			}
		}()
		return reply(app.Execute(args[0].String()))
	}))

	js.Global().Set("complete", js.FuncOf(func(this js.Value, args []js.Value) (result any) {
		if len(args) < 1 || args[0].Type() != js.TypeString {
			return map[string]any{"before": "", "matches": []any{}}
		}
		defer func() {
			if recover() != nil {
				result = map[string]any{"before": "", "matches": []any{}}
			}
		}()
		before, matches := app.CompleteLine(args[0].String())
		out := make([]any, len(matches))
		for i, m := range matches {
			out[i] = m
		}
		return map[string]any{"before": before, "matches": out}
	}))

	js.Global().Set("upload", js.FuncOf(func(this js.Value, args []js.Value) (result any) {
		if len(args) < 2 || args[0].Type() != js.TypeString {
			return fail(app, "upload: expected a path and bytes")
		}
		defer func() {
			if r := recover(); r != nil {
				result = fail(app, fmt.Sprintf("internal error: %v", r))
			}
		}()
		data := make([]byte, args[1].Get("length").Int())
		js.CopyBytesToGo(data, args[1])
		return reply(app.Upload(args[0].String(), data))
	}))

	js.Global().Set("snapshot", js.FuncOf(func(this js.Value, args []js.Value) (result any) {
		defer func() {
			if recover() != nil {
				result = js.Null()
			}
		}()
		data, err := app.Snapshot(true)
		if err != nil {
			return js.Null()
		}
		out := js.Global().Get("Uint8Array").New(len(data))
		js.CopyBytesToJS(out, data)
		return out
	}))

	js.Global().Set("loadSession", js.FuncOf(func(this js.Value, args []js.Value) (result any) {
		if len(args) < 1 {
			return false
		}
		defer func() {
			if recover() != nil {
				result = false
			}
		}()
		length := args[0].Get("byteLength").Float()
		if length > engine.MaxSnapshotBytes {
			return false
		}
		data := make([]byte, int(length))
		js.CopyBytesToGo(data, args[0])
		return app.Restore(data, true) == nil
	}))

	// Explicit shutdown is asynchronous so the browser event loop stays free
	// while background commands and WebAssembly programs finish cancelling.
	js.Global().Set("shutdown", js.FuncOf(func(this js.Value, args []js.Value) any {
		executor := js.FuncOf(func(this js.Value, args []js.Value) any {
			resolve := args[0]
			go func() {
				resolve.Invoke(shutdown())
			}()
			return nil
		})
		defer executor.Release()
		return js.Global().Get("Promise").New(executor)
	}))
	select {}
}
