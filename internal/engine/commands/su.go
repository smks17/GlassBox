package commands

import (
	"fmt"

	"glass/internal/engine/signal"
)

type Su struct{}

var _ Command = Su{}

func (c Su) Name() string { return "su" }

func (c Su) Usage() string { return "su [user] [password] — switch user (default root)" }

func (c Su) Run(ctx *Context, args []string) int {
	if len(args) > 2 {
		fmt.Fprintf(ctx.Stderr, "%s: too many operands\n", c.Name())
		return 1
	}
	name := "root"
	if len(args) >= 1 {
		name = args[0]
	}
	var password string
	if len(args) >= 2 {
		password = args[1]
	}

	// Nothing to switch: the caller is not a shell that has a user to change.
	if ctx.SwitchUser == nil {
		fmt.Fprintf(ctx.Stderr, "%s: only useful inside the shell\n", c.Name())
		return 1
	}
	if len(args) < 2 && !ctx.User.IsRoot() && ctx.ReadPassword != nil {
		var err error
		password, err = ctx.ReadPassword(ctx.Ctx, "Password: ")
		if err != nil {
			if ctx.Interrupted() {
				return 130
			}
			if code, ok := signal.Signalled(err); ok {
				return code
			}
			fmt.Fprintf(ctx.Stderr, "%s: %v\n", c.Name(), err)
			return 1
		}
	}
	if ctx.Interrupted() {
		return 130
	}
	if err := ctx.SwitchUser(name, password); err != nil {
		fmt.Fprintf(ctx.Stderr, "%s: %v\n", c.Name(), err)
		return 1
	}
	return 0
}

func init() { Register(Su{}) }
