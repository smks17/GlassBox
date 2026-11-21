package shell

import (
	"context"
	"fmt"
	"maps"
	"sync"

	"glass/internal/engine/commands"
)

type shellCommand struct{}

func (shellCommand) Name() string { return "sh" }

func (shellCommand) Usage() string { return "sh -c COMMAND" }

func (cmd shellCommand) Run(ctx *commands.Context, args []string) int {
	if len(args) != 2 || args[0] != "-c" {
		fmt.Fprintln(ctx.Stderr, "usage:", cmd.Usage())
		return 2
	}
	lifetime, cancel := context.WithCancel(ctx.Ctx)
	sh := Shell{
		fs: newDirView(ctx.VFS), vars: maps.Clone(ctx.Env), funcs: make(map[string]*List),
		proc: ctx.Self, pid: ctx.Self.PID, procHandler: ctx.Proc, clock: ctx.Clock,
		ctx: lifetime, lifetime: lifetime, workers: new(sync.WaitGroup),
		Network: ctx.Network, WasiCache: ctx.WasiCache,
	}
	sh.job = sh.newJob(0, false)
	sh.job.lifetime = lifetime
	defer func() {
		cancel()
		sh.workers.Wait()
	}()
	status := sh.Run(args[1], ctx.Fds)
	if code, exited := sh.Exiting(); exited {
		return code
	}
	return status
}

func init() { commands.Register(shellCommand{}) }
