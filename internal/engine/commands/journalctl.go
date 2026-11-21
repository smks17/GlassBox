package commands

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"glass/internal/engine/signal"
)

type Journalctl struct{}

type journalctlOptions struct {
	unit      string
	lineLimit int
}

func (Journalctl) Name() string { return "journalctl" }

func (Journalctl) Usage() string {
	return "journalctl [-n|--lines COUNT] [-u|--unit NAME.service]"
}

func (cmd Journalctl) Run(ctx *Context, args []string) int {
	options, ok := parseJournalArgs(args)
	if !ok {
		fmt.Fprintln(ctx.Stderr, "usage:", cmd.Usage())
		return 1
	}
	if ctx.Services == nil {
		fmt.Fprintln(ctx.Stderr, "journalctl: systemd is not running")
		return 1
	}
	output, err := ctx.Services.ReadJournal(options.unit, options.lineLimit)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "journalctl: read journal: %v\n", err)
		return 1
	}
	if _, err := io.WriteString(ctx.Stdout, output); err != nil {
		if status, signalled := signal.Signalled(err); signalled {
			return status
		}
		fmt.Fprintf(ctx.Stderr, "journalctl: write output: %v\n", err)
		return 1
	}
	return 0
}

func parseJournalArgs(args []string) (journalctlOptions, bool) {
	options := journalctlOptions{lineLimit: -1}
	for i := 0; i < len(args); i++ {
		flag := args[i]
		var value string
		var hasValue bool
		if strings.HasPrefix(flag, "--") {
			flag, value, hasValue = strings.Cut(flag, "=")
		} else if strings.HasPrefix(flag, "-n") && len(flag) > len("-n") {
			flag, value, hasValue = "-n", flag[len("-n"):], true
		}

		if !hasValue {
			i++
			if i == len(args) {
				return journalctlOptions{}, false
			}
			value = args[i]
		}

		switch flag {
		case "-u", "--unit":
			if value == "" {
				return journalctlOptions{}, false
			}
			options.unit = value
		case "-n", "--lines":
			count, err := strconv.Atoi(value)
			if err != nil || count < 0 {
				return journalctlOptions{}, false
			}
			options.lineLimit = count
		default:
			return journalctlOptions{}, false
		}
	}
	return options, true
}

func init() { Register(Journalctl{}) }
