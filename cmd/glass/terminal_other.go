//go:build !unix && !windows

package main

import (
	"context"
	"fmt"
	"os"
)

var suspendSignals []os.Signal
var passwordSignals = []os.Signal{os.Interrupt}

func promptTerminationSignals() []os.Signal { return []os.Signal{os.Interrupt} }

func promptSignalExitCode(os.Signal) int { return 1 }

func beginPromptInput(input, output *os.File) (func() error, error) {
	return nil, errPromptNotTerminal
}

func hidePasswordInput() (func() error, error) {
	info, err := os.Stdin.Stat()
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeCharDevice == 0 {
		return nil, nil
	}
	return nil, fmt.Errorf("hidden password input is unavailable on this platform")
}

func waitPasswordInput(context.Context) error {
	return fmt.Errorf("hidden password input is unavailable on this platform")
}
