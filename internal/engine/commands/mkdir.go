package commands

import (
	"errors"
	"fmt"
	"strings"

	"glass/internal/engine/filesystem"
	"glass/internal/engine/signal"
)

type Mkdir struct{}

var _ Command = Mkdir{}

func (mkdir Mkdir) Name() string {
	return "mkdir"
}

func (mkdir Mkdir) Usage() string {
	return "mkdir [-p] [--] dir...  — create directories (-p creates missing parents)"
}

func (mkdir Mkdir) Run(ctx *Context, args []string) int {
	parents := false
	options := true
	var directories []string
	for _, arg := range args {
		switch {
		case options && arg == "--":
			options = false
		case options && arg == "-p":
			parents = true
		case options && strings.HasPrefix(arg, "-") && arg != "-":
			fmt.Fprintf(ctx.Stderr, "%s: invalid option %q\n", mkdir.Name(), arg)
			return 1
		default:
			directories = append(directories, arg)
		}
	}
	if len(directories) == 0 {
		fmt.Fprintf(ctx.Stderr, "%s: missing operand\n", mkdir.Name())
		return 1
	}

	exit := 0
	for _, arg := range directories {
		if ctx.Interrupted() {
			return 130
		}
		var err error
		if parents {
			err = mkdirParents(ctx, arg)
		} else {
			err = ctx.VFS.Mkdir(arg)
		}
		if status, signalled := signal.Signalled(err); signalled {
			return status
		}
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "%s: %s: %v\n", mkdir.Name(), arg, err)
			exit = 1
		}
	}
	return exit
}

func mkdirParents(ctx *Context, directory string) error {
	if directory == "" {
		return filesystem.ErrNotExist
	}
	prefix := ""
	if strings.HasPrefix(directory, "/") {
		prefix = "/"
	}
	// Preserve dot components: cleaning a/../b first would skip creating and
	// checking a, which must be traversable even though the path leaves it.
	for _, component := range strings.Split(directory, "/") {
		if ctx.Interrupted() {
			return signal.ErrInterrupted
		}
		if component == "" {
			continue
		}
		prefix += component
		if err := ctx.VFS.Mkdir(prefix); err != nil {
			if !errors.Is(err, filesystem.ErrExists) {
				return err
			}
			info, err := ctx.VFS.Stat(prefix)
			if err != nil {
				return err
			}
			if !info.IsDir() {
				return filesystem.ErrNotDir
			}
		}
		prefix += "/"
	}
	return nil
}

func init() { Register(Mkdir{}) }
