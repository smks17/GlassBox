package engine

import "strings"

// CompleteLine returns the unchanged portion of a line and shell words that
// can replace its final word. An unfinished quote can be part of the prefix.
func (app *Machine) CompleteLine(line string) (string, []string) {
	start, prefix, command := completionWord(line)
	if start < 0 {
		return line, nil
	}
	matches := app.Complete(prefix, command)
	for i, match := range matches {
		matches[i] = quoteShellWord(match)
	}
	return line[:start], matches
}

func completionWord(line string) (int, string, bool) {
	var word strings.Builder
	var quote rune
	start := 0
	command, active, escaped := true, false, false
	inComment := false
	for i, ch := range line {
		if inComment {
			if ch != '\n' {
				continue
			}
			inComment = false
		}
		if escaped {
			word.WriteRune(ch)
			escaped = false
			continue
		}
		if quote != 0 {
			switch {
			case ch == quote:
				quote = 0
			case ch == '\\' && quote == '"' && i+1 < len(line) && strings.ContainsRune("\\\"$`", rune(line[i+1])):
				escaped = true
			default:
				word.WriteRune(ch)
			}
			continue
		}
		switch ch {
		case '#':
			if !active {
				inComment = true
				continue
			}
			word.WriteRune(ch)
			continue
		case ' ', '\t', '\r':
			if active {
				command = false
			}
		case '|', ';', '&', '\n', '(':
			command = true
		case '<', '>', ')':
			command = false
		default:
			active = true
			switch ch {
			case '\'', '"':
				quote = ch
			case '\\':
				escaped = true
			default:
				word.WriteRune(ch)
			}
			continue
		}
		word.Reset()
		active = false
		start = i + 1
	}
	if inComment {
		return -1, "", false
	}
	return start, word.String(), command
}

func quoteShellWord(word string) string {
	if word != "" && !strings.ContainsAny(word, " \t\r\n\\'\"$&;|<>[]()*?!#`~{}") {
		return word
	}
	return "'" + strings.ReplaceAll(word, "'", "'\\''") + "'"
}
