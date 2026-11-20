package commands

import (
	"fmt"

	"glass/internal/engine/filesystem"
)

type Readlink struct{}

func (Readlink) Name() string { return "readlink" }

func (Readlink) Usage() string { return "readlink NAME" }

func (cmd Readlink) Run(ctx *Context, args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(ctx.Stderr, "usage:", cmd.Usage())
		return 1
	}
	var target string
	err := filesystem.ErrNotImplemented
	if fs, ok := ctx.VFS.(filesystem.SymlinkFS); ok {
		target, err = fs.Readlink(args[0])
	}
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "readlink: %s: %v\n", args[0], err)
		return 1
	}
	fmt.Fprintln(ctx.Stdout, target)
	return 0
}

func init() { Register(Readlink{}) }
