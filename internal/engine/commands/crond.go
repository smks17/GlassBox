package commands

import (
	"errors"
	"fmt"
	"time"

	"glass/internal/engine/cron"
	"glass/internal/engine/filesystem"
	"glass/internal/engine/proc"
	"glass/internal/engine/user"
)

type Crond struct{}

func (Crond) Name() string { return "crond" }

func (Crond) Usage() string { return "crond — run jobs in /etc/crontab" }

func (Crond) Run(ctx *Context, args []string) int {
	if len(args) != 0 || !ctx.User.IsRoot() || ctx.Self.Info().PPID != ctx.Proc.Init().PID {
		fmt.Fprintln(ctx.Stderr, "crond: start crond.service as root")
		return 1
	}
	sh, ok := Lookup("sh")
	if !ok {
		fmt.Fprintln(ctx.Stderr, "crond: sh: command not found")
		return 1
	}
	if err := ctx.VFS.Chdir("/"); err != nil {
		fmt.Fprintf(ctx.Stderr, "crond: /: %v\n", err)
		return 1
	}
	lastMinute := ctx.Clock.Now().Truncate(time.Minute)
	for !ctx.Interrupted() {
		if err := ctx.Proc.Sleep(ctx.Self, time.Second); err != nil {
			break
		}
		minute := ctx.Clock.Now().Truncate(time.Minute)
		if !minute.After(lastMinute) {
			continue
		}
		lastMinute = minute
		entries, err := readCrontab(ctx.VFS)
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "crond: %v\n", err)
			continue
		}
		for _, source := range entries {
			if ctx.Interrupted() {
				break
			}
			runCronJob(ctx, sh, source)
		}
	}
	return 0
}

func runCronJob(ctx *Context, sh Command, source string) {
	args := []string{"-c", source}
	child := ctx.Proc.Spawn(proc.Spec{
		Parent: ctx.Self, Ctx: ctx.Ctx, User: ctx.User, Name: sh.Name(), Args: args,
		Run: func(p *proc.Process) int {
			job := *ctx
			job.Self, job.Ctx = p, p.Context()
			job.Fds = ctx.Fds.Clone()
			defer job.Fds.Destroy()
			return sh.Run(&job, args)
		},
	})
	children := []*proc.Process{child}
	ctx.Proc.Wait(children, false)
	ctx.Proc.Reap(children)
}

func readCrontab(fs filesystem.FS) ([]string, error) {
	links, ok := fs.(filesystem.SymlinkFS)
	if !ok {
		return nil, filesystem.ErrNotImplemented
	}
	for _, filename := range []string{"/etc", cron.SystemCrontabPath} {
		info, err := links.Lstat(filename)
		if errors.Is(err, filesystem.ErrNotExist) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		if info.IsDir() != (filename == "/etc") || info.Mode&(filesystem.ModeSymlink|filesystem.ModeCharDevice|0022) != 0 ||
			info.Owner.Name != user.RootName || (!info.IsDir() && info.Links != 1) {
			return nil, fmt.Errorf("%s: %w", filename, filesystem.ErrPermission)
		}
	}
	data, err := fs.Read(cron.SystemCrontabPath)
	if err != nil {
		return nil, err
	}
	return cron.ParseSystemCrontab(string(data))
}

func init() { Register(Crond{}) }
