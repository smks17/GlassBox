package cron

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

const SystemCrontabPath = "/etc/crontab"

// ParseSystemCrontab returns root commands scheduled to run every minute.
func ParseSystemCrontab(source string) ([]string, error) {
	var commands []string
	lineNumber := 0
	for line := range strings.SplitSeq(source, "\n") {
		lineNumber++
		line = strings.TrimLeftFunc(strings.TrimSuffix(line, "\r"), unicode.IsSpace)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		command, err := parseEntry(line)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", SystemCrontabPath, lineNumber, err)
		}
		commands = append(commands, command)
	}
	return commands, nil
}

func parseEntry(line string) (string, error) {
	fields := strings.Fields(line)
	if len(fields) < 7 {
		return "", errors.New("expected five schedule fields, a user, and a command")
	}
	for _, field := range fields[:5] {
		if field != "*" {
			return "", errors.New("only the every-minute schedule (* * * * *) is supported")
		}
	}
	if fields[5] != "root" {
		return "", errors.New("only root jobs are supported")
	}

	// Keep the original command text so the shell can interpret its quoting and redirections.
	for _, field := range fields[:6] {
		line = strings.TrimLeftFunc(line[len(field):], unicode.IsSpace)
	}
	return line, nil
}
