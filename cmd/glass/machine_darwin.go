//go:build darwin

package main

import "syscall"

func hostMachine() string {
	machine, err := syscall.Sysctl("hw.machine")
	if err != nil || machine == "" {
		return "unknown"
	}
	return machine
}
