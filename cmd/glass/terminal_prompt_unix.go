//go:build darwin || linux

package main

import (
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func promptTerminationSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}
}

func promptSignalExitCode(received os.Signal) int {
	if number, ok := received.(syscall.Signal); ok {
		return 128 + int(number)
	}
	return 1
}

// beginPromptInput lets the prompt read and redraw each key as it arrives.
// The returned function restores the original terminal settings after editing.
func beginPromptInput(input, output *os.File) (func() error, error) {
	// Redrawing uses terminal escape sequences, so output must also be a terminal.
	if _, err := unix.IoctlGetTermios(int(output.Fd()), promptGetMode); err != nil {
		return nil, err
	}
	fd := int(input.Fd())
	original, err := unix.IoctlGetTermios(fd, promptGetMode)
	if err != nil {
		return nil, err
	}
	mode := *original
	mode.Iflag &^= syscall.BRKINT | syscall.ICRNL | syscall.INPCK | syscall.ISTRIP | syscall.IXON
	mode.Oflag &^= syscall.OPOST
	mode.Cflag |= syscall.CS8
	// Let the editor handle echo, line submission, and control keys itself.
	mode.Lflag &^= syscall.ECHO | syscall.ICANON | syscall.IEXTEN | syscall.ISIG
	mode.Cc[syscall.VMIN] = 1
	mode.Cc[syscall.VTIME] = 0
	if err := unix.IoctlSetTermios(fd, promptSetMode, &mode); err != nil {
		return nil, err
	}
	return func() error { return unix.IoctlSetTermios(fd, promptSetMode, original) }, nil
}
