package commands

import (
	"errors"
	"fmt"

	"glass/internal/engine/filesystem"
	"glass/internal/engine/systemd"
)

type Systemctl struct{}

func (Systemctl) Name() string { return "systemctl" }

func (Systemctl) Usage() string {
	return "systemctl start|stop|restart|status|enable|disable NAME.service\n       systemctl list-units|list-unit-files"
}

func (cmd Systemctl) Run(ctx *Context, args []string) int {
	if len(args) == 1 {
		switch args[0] {
		case "list-units":
			return cmd.listUnits(ctx)
		case "list-unit-files":
			return cmd.listUnitFiles(ctx)
		}
	}
	if len(args) != 2 {
		fmt.Fprintln(ctx.Stderr, "usage:", cmd.Usage())
		return 1
	}
	verb, name := args[0], args[1]
	switch verb {
	case "start", "stop", "restart", "status", "enable", "disable":
	default:
		fmt.Fprintln(ctx.Stderr, "usage:", cmd.Usage())
		return 1
	}
	if verb != "status" && (ctx.Services == nil || !ctx.User.IsRoot()) {
		fmt.Fprintln(ctx.Stderr, "systemctl: system service control requires a running manager and root privileges")
		return 1
	}
	var status systemd.ServiceStatus
	var err error
	switch verb {
	case "start":
		err = ctx.Services.Start(name)
	case "stop":
		err = ctx.Services.Stop(name)
	case "restart":
		err = ctx.Services.Restart(name)
	case "enable":
		err = ctx.Services.Enable(name)
	case "disable":
		err = ctx.Services.Disable(name)
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
	if verb != "status" {
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

func (Systemctl) listUnits(ctx *Context) int {
	if ctx.Services == nil {
		fmt.Fprintln(ctx.Stderr, "systemctl: systemd is not running")
		return 1
	}
	fmt.Fprintf(ctx.Stdout, "%-24s %-8s %s\n", "UNIT", "PID", "DESCRIPTION")
	for _, status := range ctx.Services.ListUnits() {
		fmt.Fprintf(ctx.Stdout, "%-24s %-8d %s\n", status.Name, status.MainPID, status.Description)
	}
	return 0
}

func (Systemctl) listUnitFiles(ctx *Context) int {
	files, err := systemd.ListUnitFiles(ctx.VFS)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "systemctl: list-unit-files: %v\n", err)
		return 1
	}
	fmt.Fprintf(ctx.Stdout, "%-24s %s\n", "UNIT FILE", "STATE")
	for _, file := range files {
		fmt.Fprintf(ctx.Stdout, "%-24s %s\n", file.Name, file.State)
	}
	return 0
}

func init() { Register(Systemctl{}) }
