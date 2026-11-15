package main

import (
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
