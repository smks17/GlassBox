package commands

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"glass/internal/engine/filesystem"
	"glass/internal/engine/signal"
)

type Chmod struct{}

var _ Command = Chmod{}

func (c Chmod) Name() string { return "chmod" }

func (c Chmod) Usage() string {
	return "chmod [-R|--recursive] [--] <mode> <path>...  — change permissions (octal or symbolic; omitted class means all, without umask)"
}

func (c Chmod) Run(ctx *Context, args []string) int {
	recursive, operands := parseChmodArgs(args)

	switch len(operands) {
	case 0:
		fmt.Fprintf(ctx.Stderr, "%s: missing operand\n", c.Name())
		return 1
	case 1:
		fmt.Fprintf(ctx.Stderr, "%s: missing operand after '%s'\n", c.Name(), operands[0])
		return 1
	}

	// The whole mode expression is validated before any file is touched.
	applyMode, err := parseChmodMode(operands[0])
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "%s: invalid mode: '%s'\n", c.Name(), operands[0])
		return 1
	}

	exitCode := 0
	for _, path := range operands[1:] {
		var fatal bool
		if exitCode, fatal = foldStatus(exitCode, chmodPath(ctx, path, recursive, applyMode)); fatal {
			return exitCode
		}
	}
	return exitCode
}

// parseChmodArgs separates flags from operands. Flags are recognised until
// "--"; everything else (including symbolic modes such as "-w") is an operand.
func parseChmodArgs(args []string) (recursive bool, operands []string) {
	parsing := true
	for _, arg := range args {
		switch {
		case parsing && arg == "--":
			parsing = false
		case parsing && (arg == "-R" || arg == "--recursive"):
			recursive = true
		default:
			operands = append(operands, arg)
		}
	}
	return recursive, operands
}

// foldStatus merges one result into the running exit status. Statuses above 1
// (interrupt or signal) are fatal: the caller must stop and return immediately.
func foldStatus(total, code int) (status int, fatal bool) {
	switch {
	case code > 1:
		return code, true
	case code != 0:
		return code, false
	default:
		return total, false
	}
}

func chmodPath(ctx *Context, path string, recursive bool, applyMode chmodMode) int {
	if ctx.Interrupted() {
		return 130
	}
	entry, err := ctx.VFS.Stat(path)
	if err != nil {
		return chmodError(ctx, path, err)
	}

	exitCode := 0
	if recursive && entry.IsDir() {
		children, err := ctx.VFS.List(path)
		if err != nil {
			var fatal bool
			if exitCode, fatal = foldStatus(exitCode, chmodError(ctx, path, err)); fatal {
				return exitCode
			}
		}
		// Change children first, while their ancestors are still searchable.
		for _, child := range children {
			if child.Name == "." || child.Name == ".." {
				continue
			}
			var fatal bool
			if exitCode, fatal = foldStatus(exitCode, chmodPath(ctx, child.Path, true, applyMode)); fatal {
				return exitCode
			}
		}
	}

	if ctx.Interrupted() {
		return 130
	}
	if err := ctx.VFS.Chmod(path, applyMode(entry.Mode)); err != nil {
		exitCode, _ = foldStatus(exitCode, chmodError(ctx, path, err))
	}
	return exitCode
}

func chmodError(ctx *Context, path string, err error) int {
	if code, ok := signal.Signalled(err); ok {
		return code
	}
	fmt.Fprintf(ctx.Stderr, "chmod: %s: %v\n", path, err)
	return 1
}

var errInvalidChmodMode = errors.New("invalid mode")

// chmodMode maps a file's current mode to its new mode.
type chmodMode func(filesystem.FileMode) filesystem.FileMode

// parseMode parses an octal permission string (0–0777).
func parseMode(s string) (filesystem.FileMode, error) {
	v, err := strconv.ParseUint(s, 8, 32)
	if err != nil || v > 0777 {
		return 0, errInvalidChmodMode
	}
	return filesystem.FileMode(v), nil
}

// parseChmodMode accepts an octal or symbolic mode expression.
func parseChmodMode(expression string) (chmodMode, error) {
	if mode, err := parseMode(expression); err == nil {
		return func(current filesystem.FileMode) filesystem.FileMode {
			return current&^0777 | mode
		}, nil
	}

	var actions []chmodAction
	for _, clause := range strings.Split(expression, ",") {
		parsed, err := parseChmodClause(clause)
		if err != nil {
			return nil, err
		}
		actions = append(actions, parsed...)
	}
	return func(mode filesystem.FileMode) filesystem.FileMode {
		for _, action := range actions {
			mode = action.apply(mode)
		}
		return mode
	}, nil
}

type chmodAction struct {
	who         filesystem.FileMode // bits this action may touch
	operator    byte                // '+', '-' or '='
	permissions string              // any of "rwxX", or a single "u", "g" or "o"
}

func (a chmodAction) apply(mode filesystem.FileMode) filesystem.FileMode {
	bits := chmodPermissionBits(a.permissions, mode) & a.who
	switch a.operator {
	case '+':
		return mode | bits
	case '-':
		return mode &^ bits
	default: // '='
		return mode&^a.who | bits
	}
}

// parseChmodClause parses one comma-separated clause, e.g. "ug+rw-x".
func parseChmodClause(clause string) ([]chmodAction, error) {
	rest := strings.TrimLeft(clause, "ugoa")
	who := chmodWho(clause[:len(clause)-len(rest)])
	if rest == "" {
		return nil, errInvalidChmodMode
	}

	var actions []chmodAction
	for rest != "" {
		operator := rest[0]
		if operator != '+' && operator != '-' && operator != '=' {
			return nil, errInvalidChmodMode
		}
		rest = rest[1:]

		end := strings.IndexAny(rest, "+-=")
		if end == -1 {
			end = len(rest)
		}
		permissions := rest[:end]
		rest = rest[end:]

		if !validChmodPermissions(permissions) {
			return nil, errInvalidChmodMode
		}
		actions = append(actions, chmodAction{who, operator, permissions})
	}
	return actions, nil
}

// chmodWho converts class letters to a bit mask. The simulated machine has no
// umask, so an omitted class selects all bits.
func chmodWho(classes string) filesystem.FileMode {
	var who filesystem.FileMode
	for _, class := range classes {
		switch class {
		case 'u':
			who |= 0700
		case 'g':
			who |= 0070
		case 'o':
			who |= 0007
		case 'a':
			who |= 0777
		}
	}
	if who == 0 {
		return 0777
	}
	return who
}

func validChmodPermissions(permissions string) bool {
	for _, permission := range permissions {
		if !strings.ContainsRune("rwxXugo", permission) {
			return false
		}
	}
	// A copied class stands alone, as in g=u.
	return !strings.ContainsAny(permissions, "ugo") || len(permissions) == 1
}

// chmodPermissionBits expands permission letters into bits replicated across
// all three classes; the caller masks them down to the requested classes.
func chmodPermissionBits(permissions string, mode filesystem.FileMode) filesystem.FileMode {
	spread := func(triplet filesystem.FileMode) filesystem.FileMode {
		return triplet<<6 | triplet<<3 | triplet
	}

	var bits filesystem.FileMode
	for _, permission := range permissions {
		switch permission {
		case 'r':
			bits |= 0444
		case 'w':
			bits |= 0222
		case 'x':
			bits |= 0111
		case 'X':
			if mode.IsDirectory() || mode&0111 != 0 {
				bits |= 0111
			}
		case 'u':
			bits |= spread(mode >> 6 & 7)
		case 'g':
			bits |= spread(mode >> 3 & 7)
		case 'o':
			bits |= spread(mode & 7)
		}
	}
	return bits
}

func init() { Register(Chmod{}) }
