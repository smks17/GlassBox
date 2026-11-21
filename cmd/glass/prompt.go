package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"unicode"
)

var (
	errPromptInterrupted = errors.New("prompt interrupted")
	errPromptSuspended   = errors.New("prompt suspended")
	errPromptNotTerminal = errors.New("prompt input is not a supported terminal")
)

// Login and the prompt share one reader so pasted input is never stranded in
// a scanner's read-ahead buffer when terminal editing begins.
func readInputLine(reader *bufio.Reader) (string, error) {
	return readLine(reader)
}

func readPrompt(reader *bufio.Reader, prompt string, history []string) (line string, err error) {
	restore, modeErr := beginPromptInput(os.Stdin, os.Stdout)
	if modeErr != nil {
		if _, err := fmt.Fprint(os.Stdout, prompt); err != nil {
			return "", err
		}
		return readInputLine(reader)
	}
	// Terminal-generated Ctrl+C is read as a byte in editing mode. External
	// termination signals still need restoration before the process exits.
	var restored sync.Once
	var restoreErr error
	restoreTerminal := func() { restored.Do(func() { restoreErr = restore() }) }
	signals := make(chan os.Signal, 1)
	done := make(chan struct{})
	stopped := make(chan struct{})
	signal.Notify(signals, promptTerminationSignals()...)
	go func() {
		defer close(stopped)
		select {
		case received := <-signals:
			restoreTerminal()
			os.Exit(promptSignalExitCode(received))
		case <-done:
		}
	}()
	defer func() {
		signal.Stop(signals)
		close(done)
		<-stopped
		restoreTerminal()
		if restoreErr != nil {
			line, err = "", fmt.Errorf("restore terminal: %w", restoreErr)
		}
	}()
	return readEditedLine(reader, os.Stdout, prompt, history)
}

func readEditedLine(reader *bufio.Reader, output io.Writer, prompt string, history []string) (string, error) {
	var line, draft []rune
	index, cursor, draftCursor := len(history), 0, 0
	write := func(text string) error {
		n, err := io.WriteString(output, text)
		if err == nil && n != len(text) {
			err = io.ErrShortWrite
		}
		return err
	}
	redraw := func() error { return write("\r" + prompt + string(line) + "\x1b[K\r" + prompt + string(line[:cursor])) }
	if err := write(prompt); err != nil {
		return "", err
	}
	for {
		character, _, err := reader.ReadRune()
		if err != nil {
			if err == io.EOF && len(line) > 0 {
				return string(line), write("\r\n")
			}
			return "", err
		}
		switch character {
		case '\r', '\n':
			return string(line), write("\r\n")
		case 3:
			if err := write("^C\r\n"); err != nil {
				return "", err
			}
			return "", errPromptInterrupted
		case 4:
			if len(line) == 0 {
				return "", io.EOF
			}
			return string(line), write("\r\n")
		case 26:
			if err := write("^Z\r\n"); err != nil {
				return "", err
			}
			return "", errPromptSuspended
		case 8, 127:
			if cursor > 0 {
				line = append(line[:cursor-1], line[cursor:]...)
				cursor--
			}
		case 21:
			line, cursor = nil, 0
		case 23:
			tail := append([]rune(nil), line[cursor:]...)
			line = line[:cursor]
			for len(line) > 0 && unicode.IsSpace(line[len(line)-1]) {
				line = line[:len(line)-1]
			}
			for len(line) > 0 && !unicode.IsSpace(line[len(line)-1]) {
				line = line[:len(line)-1]
			}
			cursor = len(line)
			line = append(line, tail...)
		case 27:
			key, err := promptEscape(reader)
			if err != nil {
				return "", err
			}
			if key == "A" && index > 0 {
				if index == len(history) {
					draft = append([]rune(nil), line...)
					draftCursor = cursor
				}
				index--
				line = []rune(history[index])
				cursor = len(line)
			} else if key == "B" && index < len(history) {
				index++
				if index == len(history) {
					line = append([]rune(nil), draft...)
					cursor = draftCursor
				} else {
					line = []rune(history[index])
				}
			} else if key == "C" && cursor < len(line) {
				cursor++
			} else if key == "D" && cursor > 0 {
				cursor--
			} else if key == "3~" && cursor < len(line) {
				line = append(line[:cursor], line[cursor+1:]...)
			}
		default:
			if character == '\t' || !unicode.IsControl(character) {
				line = append(line, 0)
				copy(line[cursor+1:], line[cursor:])
				line[cursor] = character
				cursor++
			}
		}
		if err := redraw(); err != nil {
			return "", err
		}
	}
}

func promptEscape(reader *bufio.Reader) (string, error) {
	prefix, err := reader.ReadByte()
	if err != nil {
		return "", err
	}
	if prefix != '[' && prefix != 'O' {
		return "", reader.UnreadByte()
	}
	var sequence strings.Builder
	for sequence.Len() < 16 {
		character, err := reader.ReadByte()
		if err != nil {
			return "", err
		}
		sequence.WriteByte(character)
		if character >= 0x40 && character <= 0x7e {
			return sequence.String(), nil
		}
	}
	return "", nil
}
