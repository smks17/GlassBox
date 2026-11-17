package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"unicode/utf8"

	"glass/internal/engine"
)

type loginOptions struct {
	username         string
	password         string
	passwordProvided bool
}

var passwordPromptActive atomic.Bool

func (options loginOptions) validate(onDisk bool) error {
	if options.passwordProvided && options.username == "" {
		return fmt.Errorf("-password requires -username")
	}
	if onDisk && options.username != "" {
		return fmt.Errorf("login flags cannot be used with -real")
	}
	return nil
}

func login(app *engine.App, reader *bufio.Reader, options loginOptions, stdout, stderr io.Writer) bool {
	for {
		name := options.username
		if name == "" {
			fmt.Fprintf(stdout, "%s login: ", engine.ShellName)
			line, ok := scanLogin(reader, stdout, stderr)
			if !ok {
				return false
			}
			name = strings.TrimSpace(line)
			if name == "" {
				continue
			}
		}
		password := options.password
		if !options.passwordProvided {
			var ok bool
			password, ok = scanPassword(reader, stdout, stderr)
			if !ok {
				return false
			}
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

func scanPassword(reader *bufio.Reader, stdout, stderr io.Writer) (string, bool) {
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, passwordSignals...)
	defer signal.Stop(interrupts)

	restore, err := hidePasswordInput()
	if err != nil {
		fmt.Fprintf(stderr, "%s: password input: %v\n", engine.ShellName, err)
		return "", false
	}
	if restore == nil {
		signal.Stop(interrupts)
		fmt.Fprint(stdout, "Password: ")
		return scanLogin(reader, stdout, stderr)
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
	password, readErr := readLine(reader)
	if err := restore(); err != nil {
		fmt.Fprintf(stderr, "%s: restoring terminal: %v\n", engine.ShellName, err)
		return "", false
	}
	fmt.Fprintln(stdout)
	if readErr != nil && readErr != io.EOF {
		fmt.Fprintf(stderr, "%s: read error: %v\n", engine.ShellName, readErr)
	}
	return password, readErr == nil
}

func scanLogin(reader *bufio.Reader, stdout, stderr io.Writer) (string, bool) {
	line, err := readLine(reader)
	if err == nil {
		return line, true
	}
	fmt.Fprintln(stdout)
	if err != io.EOF {
		fmt.Fprintf(stderr, "%s: read error: %v\n", engine.ShellName, err)
	}
	return "", false
}

func readLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err == io.EOF && len(line) != 0 {
		err = nil
	}
	return strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r"), err
}

func passwordReader(reader *bufio.Reader) func(context.Context, string) (string, error) {
	promptLock := make(chan struct{}, 1)
	return func(ctx context.Context, prompt string) (password string, err error) {
		select {
		case promptLock <- struct{}{}:
			defer func() { <-promptLock }()
		case <-ctx.Done():
			return "", ctx.Err()
		}
		passwordPromptActive.Store(true)
		defer passwordPromptActive.Store(false)
		restore, err := preparePasswordPrompt()
		if err != nil {
			return "", err
		}
		defer func() { err = errors.Join(err, restore()) }()
		fmt.Fprint(os.Stderr, prompt)
		defer fmt.Fprintln(os.Stderr)
		return readPasswordLine(ctx, reader)
	}
}

func preparePasswordPrompt() (func() error, error) {
	interrupts := make(chan os.Signal, 1)
	for _, received := range passwordSignals {
		if received != os.Interrupt {
			signal.Notify(interrupts, received)
		}
	}
	restore, err := hidePasswordInput()
	if err != nil || restore == nil {
		signal.Stop(interrupts)
		if err == nil {
			err = fmt.Errorf("a terminal is required; use -S to read the password from stdin")
		}
		return nil, err
	}
	restore = sync.OnceValue(restore)
	done := make(chan struct{})
	go func() {
		select {
		case received := <-interrupts:
			_ = restore()
			os.Exit(128 + int(received.(syscall.Signal)))
		case <-done:
		}
	}()
	return func() error {
		defer signal.Stop(interrupts)
		defer close(done)
		return restore()
	}, nil
}

func readPasswordLine(ctx context.Context, reader *bufio.Reader) (string, error) {
	var password []byte
	tooLong := false
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if reader.Buffered() == 0 {
			if err := waitPasswordInput(ctx); err != nil {
				return "", err
			}
		}
		character, err := reader.ReadByte()
		if err != nil {
			return "", err
		}
		switch character {
		case '\r', '\n':
			if tooLong {
				return "", fmt.Errorf("password exceeds 4095 bytes")
			}
			return string(password), nil
		case '\b', 127:
			if !tooLong && len(password) != 0 {
				_, size := utf8.DecodeLastRune(password)
				password = password[:len(password)-size]
			}
		default:
			if len(password) < 4095 {
				password = append(password, character)
			} else {
				// Finish the line so rejected password text cannot become a command.
				tooLong = true
			}
		}
	}
}
