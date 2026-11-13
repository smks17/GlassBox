package commands

import (
	"fmt"
	"strings"
)

type Uname struct{}

var _ Command = Uname{}

func (Uname) Name() string { return "uname" }
func (Uname) Usage() string {
	return "uname [-asnrvmo] [--] — print static simulated system information"
}

func (Uname) Run(ctx *Context, args []string) int {
	selected := make(map[rune]bool)
	endOptions := false
	for _, arg := range args {
		if ctx.Interrupted() {
			return 130
		}
		if !endOptions && arg == "--" {
			endOptions = true
			continue
		}
		if endOptions || len(arg) < 2 || arg[0] != '-' {
			fmt.Fprintf(ctx.Stderr, "uname: %s: unexpected operand\n", arg)
			return 1
		}
		for _, flag := range arg[1:] {
			switch flag {
			case 'a':
				for _, field := range "snrvmo" {
					selected[field] = true
				}
			case 's', 'n', 'r', 'v', 'm', 'o':
				selected[flag] = true
			default:
				fmt.Fprintf(ctx.Stderr, "uname: invalid option -- '%c'\n", flag)
				return 1
			}
		}
	}
	if len(selected) == 0 {
		selected['s'] = true
	}
	// These values describe the simulated machine independently of its host.
	fields := []struct {
		option rune
		value  string
	}{
		{'s', "Parrot"}, {'n', "parrot"}, {'r', "0.1.0"},
		{'v', "virtual"}, {'m', "virtual"}, {'o', "Parrot"},
	}
	var values []string
	for _, field := range fields {
		if ctx.Interrupted() {
			return 130
		}
		if selected[field.option] {
			values = append(values, field.value)
		}
	}
	return identityMetadataWrite(ctx, "uname", strings.Join(values, " ")+"\n")
}

func init() { Register(Uname{}) }
