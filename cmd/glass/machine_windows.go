//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

// nativeSystemInfo matches Windows SYSTEM_INFO, including pointer-sized fields
// so the native API can safely fill it in both 32-bit and 64-bit processes.
type nativeSystemInfo struct {
	architecture              uint16
	reserved                  uint16
	pageSize                  uint32
	minimumApplicationAddress uintptr
	maximumApplicationAddress uintptr
	activeProcessorMask       uintptr
	numberOfProcessors        uint32
	processorType             uint32
	allocationGranularity     uint32
	processorLevel            uint16
	processorRevision         uint16
}

func hostMachine() string {
	kernel := syscall.NewLazyDLL("kernel32.dll")
	// This API identifies the native machine even under Windows on ARM
	// emulation, where GetNativeSystemInfo may describe the emulated CPU.
	queryProcess := kernel.NewProc("IsWow64Process2")
	if err := queryProcess.Find(); err == nil {
		if process, err := syscall.GetCurrentProcess(); err == nil {
			var processMachine, nativeMachine uint16
			ok, _, _ := queryProcess.Call(uintptr(process), uintptr(unsafe.Pointer(&processMachine)), uintptr(unsafe.Pointer(&nativeMachine)))
			if ok != 0 {
				return windowsImageMachine(nativeMachine)
			}
		}
	}
	query := kernel.NewProc("GetNativeSystemInfo")
	if err := query.Find(); err != nil {
		return "unknown"
	}
	info := nativeSystemInfo{architecture: 0xffff}
	query.Call(uintptr(unsafe.Pointer(&info)))
	return windowsMachine(info.architecture)
}

func windowsImageMachine(machine uint16) string {
	switch machine {
	case 0x014c: // IMAGE_FILE_MACHINE_I386
		return "i686"
	case 0x01c4: // IMAGE_FILE_MACHINE_ARMNT
		return "arm"
	case 0x8664: // IMAGE_FILE_MACHINE_AMD64
		return "x86_64"
	case 0xaa64: // IMAGE_FILE_MACHINE_ARM64
		return "arm64"
	default:
		return "unknown"
	}
}

func windowsMachine(architecture uint16) string {
	switch architecture {
	case 0: // PROCESSOR_ARCHITECTURE_INTEL
		return "i686"
	case 5: // PROCESSOR_ARCHITECTURE_ARM
		return "arm"
	case 9: // PROCESSOR_ARCHITECTURE_AMD64
		return "x86_64"
	case 12: // PROCESSOR_ARCHITECTURE_ARM64
		return "arm64"
	default:
		return "unknown"
	}
}
