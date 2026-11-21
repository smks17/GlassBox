package shell

import (
	"fmt"

	"glass/internal/engine/commands"
	"glass/internal/engine/filesystem"
	"glass/internal/engine/proc"
	"glass/internal/engine/stream"
	"glass/internal/engine/systemd"
)

func (sh *Shell) PrepareServiceCommand(fs filesystem.FS, words []string) (func(*proc.Process, *stream.FDTable) int, error) {
	command, ok := commands.Lookup(words[0])
	if !ok {
		return nil, fmt.Errorf("%w %s", systemd.ErrUnknownExecutable, words[0])
	}
	service := Shell{
		fs: newDirView(fs), procHandler: sh.procHandler,
		vars:  map[string]string{commands.EnvPath: commands.DefaultPath},
		clock: sh.clock, tick: sh.tick, Network: sh.Network, WasiCache: sh.WasiCache,
	}
	return func(p *proc.Process, fds *stream.FDTable) int {
		service.proc, service.ctx = p, p.Context()
		return command.Run(service.context(fds), words[1:])
	}, nil
}
