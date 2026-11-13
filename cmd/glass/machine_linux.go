//go:build linux

package main

import "syscall"

func hostMachine() string {
	var info syscall.Utsname
	if err := syscall.Uname(&info); err != nil {
		return "unknown"
	}
	var machine []byte
	for _, character := range info.Machine {
		if character == 0 {
			break
		}
		machine = append(machine, byte(character))
	}
	if len(machine) == 0 {
		return "unknown"
	}
	return string(machine)
}
