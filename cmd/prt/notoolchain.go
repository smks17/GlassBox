//go:build !toolchain

package main

import "parrot/internal/engine"

// installToolchain has nothing to install in a build without -tags
// toolchain; see toolchain.go.
func installToolchain(*engine.App) error { return nil }
