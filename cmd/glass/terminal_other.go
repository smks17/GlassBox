//go:build !unix && !windows

package main

import (
	"fmt"
	"os"
)

var suspendSignals []os.Signal
var passwordSignals = []os.Signal{os.Interrupt}

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
