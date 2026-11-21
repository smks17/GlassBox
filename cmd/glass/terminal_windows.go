package main

import (
	"context"
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

var suspendSignals []os.Signal
var passwordSignals = []os.Signal{os.Interrupt}

func hidePasswordInput() (func() error, error) {
	handle := windows.Handle(os.Stdin.Fd())
	var original uint32
	if err := windows.GetConsoleMode(handle, &original); err != nil {
		if errors.Is(err, windows.ERROR_INVALID_HANDLE) {
			return nil, nil
		}
		return nil, err
	}
	if err := windows.SetConsoleMode(handle, original&^windows.ENABLE_ECHO_INPUT); err != nil {
		return nil, err
	}
	return func() error { return windows.SetConsoleMode(handle, original) }, nil
}

func waitPasswordInput(ctx context.Context) error {
	handle := windows.Handle(os.Stdin.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(handle, &mode); err != nil {
		return err
	}
	// Read each key so cancellation does not wait for a complete console line.
	if err := windows.SetConsoleMode(handle, mode&^windows.ENABLE_LINE_INPUT); err != nil {
		return err
	}
	for ctx.Err() == nil {
		status, err := windows.WaitForSingleObject(handle, 100)
		if err != nil {
			return err
		}
		if status == windows.WAIT_OBJECT_0 {
			return nil
		}
	}
	return ctx.Err()
}
