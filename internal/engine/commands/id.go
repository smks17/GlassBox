package commands

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"parrot/internal/engine/signal"
	"parrot/internal/engine/user"
)

type Id struct{}

var _ Command = Id{}

func (Id) Name() string { return "id" }
func (Id) Usage() string {
	return "id [-u|-g|-G] [-n] [-r] [--] — print the current simulated identity"
}

func (Id) Run(ctx *Context, args []string) int {
	var selector rune
	var names, real, endOptions bool
	for _, arg := range args {
		if ctx.Interrupted() {
			return 130
		}
		if !endOptions && arg == "--" {
			endOptions = true
			continue
		}
		if endOptions || len(arg) < 2 || arg[0] != '-' {
			fmt.Fprintf(ctx.Stderr, "id: %s: named-user lookup is not supported\n", arg)
			return 1
		}
		for _, flag := range arg[1:] {
			switch flag {
			case 'u', 'g', 'G':
				if selector != 0 && selector != flag {
					fmt.Fprintln(ctx.Stderr, "id: conflicting identity selectors")
					return 1
				}
				selector = flag
			case 'n':
				names = true
			case 'r':
				real = true
			default:
				fmt.Fprintf(ctx.Stderr, "id: invalid option -- '%c'\n", flag)
				return 1
			}
		}
	}
	if selector == 0 && (names || real) {
		fmt.Fprintln(ctx.Stderr, "id: -n and -r require -u, -g or -G")
		return 1
	}
	if ctx.Interrupted() {
		return 130
	}
	id := ctx.VFS.Identity()
	// An absent primary group record must not borrow a supplementary name.
	groups := []user.Group{{GID: id.GID}}
	seen := map[int]bool{id.GID: true}
	for _, group := range id.Groups {
		if ctx.Interrupted() {
			return 130
		}
		if group.GID == id.GID {
			groups[0] = group
		} else if !seen[group.GID] {
			groups = append(groups, group)
			seen[group.GID] = true
		}
	}
	var result string
	switch selector {
	case 'u':
		result = idSelectedValue(id.UID, id.Name, names)
	case 'g':
		result = idSelectedValue(id.GID, groups[0].Name, names)
	case 'G':
		values := make([]string, 0, len(groups))
		for _, group := range groups {
			if ctx.Interrupted() {
				return 130
			}
			values = append(values, idSelectedValue(group.GID, group.Name, names))
		}
		result = strings.Join(values, " ")
	default:
		values := make([]string, 0, len(groups))
		for _, group := range groups {
			if ctx.Interrupted() {
				return 130
			}
			values = append(values, idDescribedValue(group.GID, group.Name))
		}
		result = fmt.Sprintf("uid=%s gid=%s groups=%s", idDescribedValue(id.UID, id.Name), idDescribedValue(id.GID, groups[0].Name), strings.Join(values, ","))
	}
	return identityMetadataWrite(ctx, "id", result+"\n")
}

func idSelectedValue(number int, name string, names bool) string {
	if names && name != "" {
		return name
	}
	return strconv.Itoa(number)
}

func idDescribedValue(number int, name string) string {
	value := strconv.Itoa(number)
	if name != "" {
		value += "(" + name + ")"
	}
	return value
}

func identityMetadataWrite(ctx *Context, command, output string) int {
	if ctx.Interrupted() {
		return 130
	}
	written, err := io.WriteString(ctx.Stdout, output)
	if err == nil && written != len(output) {
		err = io.ErrShortWrite
	}
	if err == nil {
		return 0
	}
	if code, ok := signal.Signalled(err); ok {
		return code
	}
	fmt.Fprintf(ctx.Stderr, "%s: %v\n", command, err)
	return 1
}

func init() { Register(Id{}) }
