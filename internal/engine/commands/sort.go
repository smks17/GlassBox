package commands

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"glass/internal/engine/signal"
)

type Sort struct{}

var _ Command = Sort{}

func (Sort) Name() string  { return "sort" }
func (Sort) Usage() string { return "sort [file]  — sort lines alphabetically" }

func (c Sort) Run(ctx *Context, args []string) int {
	if len(args) > 1 {
		fmt.Fprintf(ctx.Stderr, "%s: too many operands\n", c.Name())
		return 1
	}

	var in io.Reader = strings.NewReader("")
	if len(args) == 1 {
		file, err := ctx.VFS.OpenDefault(args[0])
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "%s: %s: %v\n", c.Name(), args[0], err)
			return 1
		}
		defer file.Close()
		in = file
	} else if ctx.Stdin != nil {
		in = ctx.Stdin
	}

	data, err := io.ReadAll(in)
	if status, signalled := signal.Signalled(err); signalled {
		return status
	}
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "%s: %v\n", c.Name(), err)
		return 1
	}
	if len(data) == 0 {
		return 0
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	slices.Sort(lines)
	for _, line := range lines {
		if ctx.Interrupted() {
			return 130
		}
		if _, err := fmt.Fprintln(ctx.Stdout, line); err != nil {
			if status, signalled := signal.Signalled(err); signalled {
				return status
			}
			fmt.Fprintf(ctx.Stderr, "%s: %v\n", c.Name(), err)
			return 1
		}
	}
	return 0
}

func init() { Register(Sort{}) }
