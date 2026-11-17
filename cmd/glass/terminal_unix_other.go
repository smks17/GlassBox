//go:build unix && !linux && !darwin

package main

import (
	"context"
	"fmt"
	"os"
	"syscall"
)

// suspendSignals is what Ctrl+Z sends: the terminal's SIGTSTP. Caught only
// while a command runs, so at the prompt Ctrl+Z still stops glass itself.
var suspendSignals = []os.Signal{syscall.SIGTSTP}

var passwordSignals = []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT}

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
