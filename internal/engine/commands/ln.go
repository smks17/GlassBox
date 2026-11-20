package commands

import (
	"fmt"

	"glass/internal/engine/filesystem"
)

type Ln struct{}

var _ Command = Ln{}

func (ln Ln) Name() string { return "ln" }

func (ln Ln) Usage() string {
	return "ln [-s] TARGET NAME"
}

// Run is link(2): a second entry pointing at one inode. Nothing is copied, so
// the two names are not two files — "ls -i" shows one inode number and a link
// count of 2, and writing through either is writing the same bytes.
func (ln Ln) Run(ctx *Context, args []string) int {
	var operands []string
	var symbolic bool
	for _, arg := range args {
		if arg == "-s" || arg == "--symbolic" {
			symbolic = true
			continue
		}
		operands = append(operands, arg)
	}

	if len(operands) != 2 {
		fmt.Fprintln(ctx.Stderr, "usage:", ln.Usage())
		return 1
	}
	target, name := operands[0], operands[1]

	var err error
	if symbolic {
		err = filesystem.ErrNotImplemented
		if fs, ok := ctx.VFS.(filesystem.SymlinkFS); ok {
			err = fs.Symlink(target, name)
		}
	} else {
		err = ctx.VFS.Link(target, name)
	}
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "%s: %s: %v\n", ln.Name(), name, err)
		return 1
	}
	return 0
}

func init() { Register(Ln{}) }
