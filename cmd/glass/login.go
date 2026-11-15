package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"glass/internal/engine"
)

type loginOptions struct {
	username         string
	password         string
	passwordProvided bool
}

func (options loginOptions) validate(onDisk bool) error {
	if options.passwordProvided && options.username == "" {
		return fmt.Errorf("-password requires -username")
	}
	if onDisk && options.username != "" {
		return fmt.Errorf("login flags cannot be used with -real")
	}
	return nil
}

func login(app *engine.App, scanner *bufio.Scanner, options loginOptions, stdout, stderr io.Writer) bool {
	for {
		name := options.username
		if name == "" {
			fmt.Fprintf(stdout, "%s login: ", engine.ShellName)
			if !scanLogin(scanner, stdout, stderr) {
				return false
			}
			name = strings.TrimSpace(scanner.Text())
			if name == "" {
				continue
			}
		}
		password := options.password
		if !options.passwordProvided {
			if !scanPassword(scanner, stdout, stderr) {
				return false
			}
			password = scanner.Text()
		}
		if err := app.Login(name, password); err != nil {
			fmt.Fprintln(stderr, "Login incorrect")
			// Fixed credentials cannot succeed on a retry without new input.
			if options.passwordProvided {
				return false
			}
			continue
		}
		return true
	}
}

func scanPassword(scanner *bufio.Scanner, stdout, stderr io.Writer) bool {
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, passwordSignals...)
	defer signal.Stop(interrupts)

	restore, err := hidePasswordInput()
	if err != nil {
		fmt.Fprintf(stderr, "%s: password input: %v\n", engine.ShellName, err)
		return false
	}
	if restore == nil {
		signal.Stop(interrupts)
		fmt.Fprint(stdout, "Password: ")
		return scanLogin(scanner, stdout, stderr)
	}
	// An interrupt and normal completion may race to restore terminal echo.
	restore = sync.OnceValue(restore)
	defer restore()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case received := <-interrupts:
			_ = restore()
			fmt.Fprintln(stdout)
			if number, ok := received.(syscall.Signal); ok {
				os.Exit(128 + int(number))
			}
			os.Exit(130)
		case <-done:
		}
	}()

	// Show the prompt only after echo is disabled, so immediate input stays hidden.
	fmt.Fprint(stdout, "Password: ")
	scanned := scanner.Scan()
	if err := restore(); err != nil {
		fmt.Fprintf(stderr, "%s: restoring terminal: %v\n", engine.ShellName, err)
		return false
	}
	fmt.Fprintln(stdout)
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(stderr, "%s: read error: %v\n", engine.ShellName, err)
	}
	return scanned
}

func scanLogin(scanner *bufio.Scanner, stdout, stderr io.Writer) bool {
	if scanner.Scan() {
		return true
	}
	fmt.Fprintln(stdout)
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(stderr, "%s: read error: %v\n", engine.ShellName, err)
	}
	return false
}
