package commands

import (
	"errors"
	"fmt"

	"glass/internal/engine/filesystem"
	"glass/internal/engine/systemd"
)

type Systemctl struct{}

func (Systemctl) Name() string { return "systemctl" }

func (Systemctl) Usage() string { return "systemctl start|status|stop NAME.service" }

func (cmd Systemctl) Run(ctx *Context, args []string) int {
	if len(args) != 2 || (args[0] != "status" && args[0] != "start" && args[0] != "stop") {
		fmt.Fprintln(ctx.Stderr, "usage:", cmd.Usage())
		return 1
	}
	name := args[1]
	if args[0] != "status" {
		if ctx.Services == nil || !ctx.User.IsRoot() {
			fmt.Fprintln(ctx.Stderr, "systemctl: system service control requires a running manager and root privileges")
			return 1
		}
	}
	var status systemd.ServiceStatus
	var err error
	switch args[0] {
	case "start":
		err = ctx.Services.Start(name)
	case "stop":
		err = ctx.Services.Stop(name)
	case "status":
		if ctx.Services == nil {
			status, err = systemd.FileStatus(ctx.VFS, name)
		} else {
			status, err = ctx.Services.Status(name)
		}
	}
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "systemctl: %s: %v\n", name, err)
		if errors.Is(err, filesystem.ErrNotExist) {
			return 4
		}
		return 1
	}
	if args[0] != "status" {
		return 0
	}
	fmt.Fprint(ctx.Stdout, name)
	if status.Description != "" {
		fmt.Fprint(ctx.Stdout, " - ", status.Description)
	}
	fmt.Fprintf(ctx.Stdout, "\nLoaded: %s\n", status.Path)
	if status.MainPID != 0 {
		fmt.Fprintf(ctx.Stdout, "Active: active (running)\nMain PID: %d\n", status.MainPID)
		return 0
	}
	fmt.Fprintln(ctx.Stdout, "Active: inactive")
	return 3
}

func init() { Register(Systemctl{}) }
