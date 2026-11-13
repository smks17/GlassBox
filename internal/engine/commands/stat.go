package commands

import (
	"fmt"
	"strings"
)

type Stat struct{}

var _ Command = Stat{}

func (Stat) Name() string { return "stat" }
func (Stat) Usage() string {
	return "stat [--] path... — print simulated file metadata (default format only)"
}

func (Stat) Run(ctx *Context, args []string) int {
	var paths []string
	endOptions := false
	for _, arg := range args {
		if ctx.Interrupted() {
			return 130
		}
		if !endOptions && arg == "--" {
			endOptions = true
			continue
		}
		if !endOptions && len(arg) > 1 && strings.HasPrefix(arg, "-") {
			fmt.Fprintf(ctx.Stderr, "stat: %s: unsupported option\n", arg)
			return 1
		}
		paths = append(paths, arg)
	}
	if ctx.Interrupted() {
		return 130
	}
	if len(paths) == 0 {
		fmt.Fprintln(ctx.Stderr, "stat: missing operand")
		return 1
	}
	status := 0
	for _, path := range paths {
		if ctx.Interrupted() {
			return 130
		}
		info, err := ctx.VFS.Stat(path)
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "stat: %s: %v\n", path, err)
			status = 1
			continue
		}
		kind := "regular file"
		if info.IsDir() {
			kind = "directory"
		} else if info.Mode.IsCharDevice() {
			kind = "character special file"
		}
		// VFS metadata provides owner names without numeric account IDs.
		output := fmt.Sprintf("  File: %s\n  Size: %d\tType: %s\n Inode: %d\tLinks: %d\nAccess: (%04o/%s)\n  User: %s\tGroup: %s\nModify: %s\n",
			path, info.Size, kind, info.Ino, info.Links, info.Mode&0777, info.Mode,
			info.Owner.Name, info.Owner.Primary(), info.ModTime.Format("2006-01-02 15:04:05 -0700"))
		if code := identityMetadataWrite(ctx, "stat", output); code != 0 {
			return code
		}
	}
	return status
}

func init() { Register(Stat{}) }
