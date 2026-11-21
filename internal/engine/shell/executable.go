package shell

import (
	"errors"
	"io"
	"path"
	"strings"

	"glass/internal/engine/commands"
	"glass/internal/engine/filesystem"
	"glass/internal/engine/stream"
	"glass/internal/engine/user"
)

const wasmMagic = "\x00asm"

var errExecFormat = errors.New("cannot execute binary file: Exec format error")

func executable(info filesystem.Info, id user.Identity) bool {
	if info.Mode&0111 == 0 { // TODO: refactor
		return false
	}
	owner := filesystem.Ownership{User: info.Owner.Name}
	if len(info.Owner.Groups) > 0 {
		owner.Group = info.Owner.Groups[0].Name
	}
	return filesystem.CheckPerm(info.Mode, owner, id, filesystem.PermExec) == nil
}

func isWasm(fs filesystem.FS, p string) bool {
	file, err := fs.OpenDefault(p)
	if err != nil {
		return false
	}
	defer file.Close()
	magic := make([]byte, len(wasmMagic))
	if _, err := io.ReadFull(file, magic); err != nil {
		return false
	}
	return string(magic) == wasmMagic
}

func (sh *Shell) lookPath(name string, fds *stream.FDTable) (commands.Command, bool) {
	dirs, set := sh.vars[commands.EnvPath]
	if !set {
		dirs = commands.DefaultPath // as bash does when PATH is unset
	}
	for _, dir := range strings.Split(dirs, ":") {
		if dir == "" {
			dir = "." // an empty entry in PATH is the current directory
		}
		if cmd, _, err := sh.program(path.Join(dir, name), fds); err == nil {
			return cmd, true
		}
	}
	return nil, false
}

// program is execve(2) for the shell
func (sh *Shell) program(p string, fds *stream.FDTable) (commands.Command, int, error) {
	fs := viewFor(sh.fs, fds)
	info, err := fs.Stat(p)
	if err != nil {
		return nil, 127, err
	}
	if info.IsDir() {
		return nil, 126, filesystem.ErrIsDir
	}
	if !executable(info, fs.Identity()) {
		return nil, 126, filesystem.ErrPermission
	}
	if !isWasm(fs, p) {
		return nil, 126, errExecFormat
	}
	return commands.WasmProgram{Path: p}, 0, nil
}

func execError(err error) string {
	switch {
	case errors.Is(err, filesystem.ErrNotExist):
		return "No such file or directory"
	case errors.Is(err, filesystem.ErrIsDir):
		return "Is a directory"
	case errors.Is(err, filesystem.ErrPermission):
		return "Permission denied"
	case errors.Is(err, filesystem.ErrNotDir):
		return "Not a directory"
	}
	return err.Error()
}
