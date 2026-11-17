package commands

import (
	"fmt"
	"io"
	"strings"

	"glass/internal/engine/signal"
	"glass/internal/engine/user"
)

type Sudo struct{}

var _ Command = Sudo{}

func (Sudo) Name() string { return "sudo" }
func (Sudo) Usage() string {
	return "sudo [-S] [-u user] [--] command [args...] — run a command as another user"
}

func (c Sudo) Run(ctx *Context, args []string) int {
	target := user.RootName
	fromStdin := false
	for len(args) > 0 {
		option := args[0]
		if option == "--" {
			args = args[1:]
			break
		}
		if !strings.HasPrefix(option, "-") {
			break
		}
		args = args[1:]
		switch option {
		case "-S":
			fromStdin = true
		case "-u":
			if len(args) == 0 || args[0] == "" {
				fmt.Fprintln(ctx.Stderr, "sudo: -u requires a username")
				return 1
			}
			target, args = args[0], args[1:]
		default:
			fmt.Fprintf(ctx.Stderr, "sudo: unknown option: %s\n", option)
			return 1
		}
	}
	if len(args) == 0 {
		fmt.Fprintln(ctx.Stderr, c.Usage())
		return 1
	}
	if ctx.RunSudo == nil {
		fmt.Fprintln(ctx.Stderr, "sudo: unavailable in this session")
		return 1
	}
	if ctx.Interrupted() {
		return 130
	}
	return ctx.RunSudo(target, args, func() (string, error) {
		var password string
		var err error
		if fromStdin {
			password, err = sudoPasswordLine(ctx)
		} else if ctx.ReadPassword != nil {
			password, err = ctx.ReadPassword(ctx.Ctx, fmt.Sprintf("[sudo] password for %s: ", ctx.User.Name))
		} else {
			err = fmt.Errorf("a terminal is required; use -S to read the password from stdin")
		}
		return password, err
	})
}

func sudoPasswordLine(ctx *Context) (string, error) {
	if ctx.Stdin == nil {
		return "", fmt.Errorf("no password was provided")
	}
	// Read one byte at a time so the target keeps all input after the password.
	var password []byte
	var one [1]byte
	for len(password) < 4096 {
		if ctx.Interrupted() {
			return "", signal.ErrInterrupted
		}
		n, err := ctx.Stdin.Read(one[:])
		if n != 0 {
			if one[0] == '\n' {
				return strings.TrimSuffix(string(password), "\r"), nil
			}
			password = append(password, one[0])
		}
		if err == io.EOF {
			if len(password) != 0 {
				return string(password), nil
			}
			return "", fmt.Errorf("no password was provided")
		}
		if err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("password exceeds 4095 bytes")
}

func init() { Register(Sudo{}) }
