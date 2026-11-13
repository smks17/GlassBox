package commands

import (
	"errors"
	"fmt"
	"path"
	"strings"

	"parrot/internal/engine/wasi"
)

type Program struct{}

var _ Command = Program{}

func (c Program) Name() string { return "program" }

func (c Program) Usage() string { return "program PROGRAM [ARGS...]  — run a WASI program" }

func (c Program) Hidden() bool { return true }

const defaultPath = "/usr/bin:/bin"

const wasiLib = "/usr/lib/wasm32-wasip1"

func (c Program) Run(ctx *Context, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(ctx.Stderr, "wasi: missing program")
		return 2
	}
	program, err := lookPath(ctx, args[0])
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "wasi: %s: %v\n", args[0], err)
		return 127
	}
	if path.Base(program) == "clang" && linking(args[1:]) {
		return link(ctx, program, args[1:])
	}
	return run(ctx, program, args[1:])
}

func lookPath(ctx *Context, name string) (string, error) {
	if strings.Contains(name, "/") {
		return name, nil
	}
	dirs := ctx.Env["PATH"]
	if dirs == "" {
		dirs = defaultPath
	}
	for _, dir := range strings.Split(dirs, ":") {
		candidate := path.Join(dir, name)
		if info, err := ctx.VFS.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", errors.New("command not found")
}

func run(ctx *Context, program string, args []string) int {
	// The whole binary is needed: it is compiled before it can run.
	module, err := ctx.VFS.Read(program)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "wasi: %s: %v\n", program, err)
		return 127
	}
	argv := append([]string{path.Base(program)}, args...)
	return wasi.Run(ctx.Ctx, module, wasi.WasiContext{
		FS:     ctx.VFS,
		Stdin:  ctx.Stdin,
		Stdout: ctx.Stdout,
		Stderr: ctx.Stderr,
		Args:   argv,
		Env:    ctx.Env,
		Cache:  ctx.WasiCache,
	})
}

func linking(args []string) bool {
	for _, a := range args {
		switch a {
		case "-c", "-S", "-E", "-M", "-MM", "-fsyntax-only",
			"-v", "--version", "-###", "-help", "--help":
			return false
		}
	}
	return true
}

func link(ctx *Context, clang string, args []string) int {
	out := "a.out"
	var sources, objects, compileFlags, linkFlags []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-o" && i+1 < len(args):
			out = args[i+1]
			i++
		case strings.HasPrefix(a, "-o") && len(a) > 2:
			out = a[2:]
		case strings.HasPrefix(a, "-l"), strings.HasPrefix(a, "-L"):
			linkFlags = append(linkFlags, a)
		case strings.HasPrefix(a, "-Wl,"):
			linkFlags = append(linkFlags, strings.Split(a[len("-Wl,"):], ",")...)
		case strings.HasPrefix(a, "-"):
			compileFlags = append(compileFlags, a)
		case isSource(a):
			sources = append(sources, a)
		default: // objects and archives, already compiled
			objects = append(objects, a)
		}
	}
	if len(sources)+len(objects) == 0 {
		return run(ctx, clang, args) // let clang say "no input files"
	}

	ld, err := lookPath(ctx, "wasm-ld")
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "wasi: wasm-ld: %v\n", err)
		return 127
	}

	var temps []string
	defer func() {
		for _, t := range temps {
			_ = ctx.VFS.Remove(t, false)
		}
	}()
	for _, src := range sources {
		obj := path.Join(path.Dir(out), "."+path.Base(out)+"-"+path.Base(src)+".o")
		temps = append(temps, obj)
		compile := append(append([]string{}, compileFlags...), "-c", src, "-o", obj)
		if status := run(ctx, clang, compile); status != 0 {
			return status
		}
		objects = append(objects, obj)
	}

	// The order matters: start-up code first, then the program, then the
	// libraries that resolve what the program still needs.
	ldArgs := []string{wasiLib + "/crt1-command.o", wasiLib + "/pwd.o"}
	ldArgs = append(ldArgs, objects...)
	ldArgs = append(ldArgs, linkFlags...)
	ldArgs = append(ldArgs, wasiLib+"/libc.a", wasiLib+"/libclang_rt.builtins.a", "-o", out)
	if status := run(ctx, ld, ldArgs); status != 0 {
		return status
	}
	if err := ctx.VFS.Chmod(out, 0755); err != nil {
		fmt.Fprintf(ctx.Stderr, "wasi: %s: %v\n", out, err)
		return 1
	}
	return 0
}

func isSource(name string) bool {
	switch path.Ext(name) {
	case ".c", ".cc", ".cpp", ".cxx", ".s", ".S":
		return true
	}
	return false
}

func init() { Register(Program{}) }
