//go:build toolchain

package main

import (
	"embed"
	"io/fs"
	"strings"

	"parrot/internal/engine"
	"parrot/internal/engine/filesystem"
)

// toolchain is the C compiler and what it links against, carried inside the
// binary when it is built with -tags toolchain. scripts/parrot.sh lays the
// folder out as the tree it becomes in the machine: toolchain/usr/bin/clang
// is /usr/bin/clang.
//
//go:embed all:toolchain
var toolchain embed.FS

// installToolchain copies the embedded tree into the machine at boot, owned
// by root. Programs in a bin directory are made executable.
func installToolchain(app *engine.App) error {
	return fs.WalkDir(toolchain, "toolchain", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := toolchain.ReadFile(p)
		if err != nil {
			return err
		}
		target := strings.TrimPrefix(p, "toolchain")
		mode := filesystem.FileMode(0644)
		if strings.Contains(target, "/bin/") {
			mode = 0755
		}
		return app.Install(target, data, mode)
	})
}
