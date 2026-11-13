//go:build !darwin && !linux && !windows

package main

func hostMachine() string { return "unknown" }
