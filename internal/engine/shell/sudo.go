package shell

import (
	"fmt"
	"strconv"

	"glass/internal/engine/commands"
	"glass/internal/engine/signal"
	"glass/internal/engine/stream"
	"glass/internal/engine/user"
)

func (sh *Shell) runSudo(target string, args []string, readPassword func() (string, error), fds *stream.FDTable) int {
	caller := sh.fs.Identity()
	identity, err := sh.AuthorizeSudo(caller, target, readPassword)
	if err != nil {
		if sh.ctx.Err() != nil {
			return 130
		}
		if code, ok := signal.Signalled(err); ok {
			return code
		}
		fmt.Fprintf(fds.Stderr(), "sudo: %v\n", err)
		return 1
	}
	if sh.ctx.Err() != nil {
		return 130
	}
	if args[0] == "cd" || args[0] == "su" {
		fmt.Fprintf(fds.Stderr(), "sudo: %s: cannot run a session builtin\n", args[0])
		return 127
	}
	child := sh.sub()
	view := child.fs.(*dirView)
	setter, ok := view.FS.(interface{ SetIdentity(user.Identity) })
	if !ok {
		fmt.Fprintln(fds.Stderr(), "sudo: unavailable on this filesystem")
		return 1
	}
	setter.SetIdentity(identity)
	// The next process receives this identity through its process specification.
	// SetUser would mutate the shared parent process as well.
	child.user = identity
	child.funcs = nil
	child.Fallback = nil
	child.vars[commands.EnvUser] = identity.Name
	child.vars[commands.EnvHome] = identity.Home
	child.vars[commands.EnvPath] = commands.DefaultPath
	child.vars["SUDO_USER"] = caller.Name
	child.vars["SUDO_UID"] = strconv.Itoa(caller.UID)
	child.vars["SUDO_GID"] = strconv.Itoa(caller.GID)
	return child.runExecutable(args, fds)
}
