package systemd

import (
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"

	"glass/internal/engine/filesystem"
	"glass/internal/engine/user"
	"glass/internal/engine/vfs"
)

const (
	bootTarget        = "multi-user.target"
	bootWantsDir      = vfs.SystemUnitDir + "/" + bootTarget + ".wants"
	legacyEnabledPath = vfs.SystemUnitDir + "/enabled"
)

func (m *control) Enable(name string) error {
	return m.setEnabled(name, true)
}

func (m *control) Disable(name string) error {
	return m.setEnabled(name, false)
}

func (m *control) setEnabled(name string, enabled bool) error {
	if !m.fs.Identity().IsRoot() {
		return filesystem.ErrPermission
	}
	if !validServiceName(name) {
		return fmt.Errorf("%w %q", ErrInvalidServiceName, name)
	}
	links, ok := m.fs.(filesystem.SymlinkFS)
	if !ok {
		return filesystem.ErrNotImplemented
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := checkUnitDirectories(links); err != nil {
		return err
	}
	if err := migrateEnabledUnits(m.fs, links); err != nil {
		return err
	}
	if enabled {
		if err := checkSystemPath(links, path.Join(vfs.SystemUnitDir, name), false); err != nil {
			return err
		}
		service, err := LoadService(m.fs, name)
		if err != nil {
			return err
		}
		if !slices.Contains(service.WantedBy, bootTarget) {
			return ErrNoInstallTarget
		}
		if err := ensureWantsDir(m.fs, links); err != nil {
			return err
		}
		return linkEnabledUnit(links, name)
	}
	if err := checkSystemPath(links, bootWantsDir, true); err != nil {
		if errors.Is(err, filesystem.ErrNotExist) {
			return nil
		}
		return err
	}
	if err := checkEnabledLink(links, name); err != nil {
		if errors.Is(err, filesystem.ErrNotExist) {
			return nil
		}
		return err
	}
	return m.fs.Remove(path.Join(bootWantsDir, name), false)
}

// StartEnabled starts each service wanted by the boot target.
func (m *Manager) StartEnabled(fs filesystem.FS) {
	links, ok := fs.(filesystem.SymlinkFS)
	if !ok || !fs.Identity().IsRoot() {
		m.logBootError("", filesystem.ErrPermission)
		return
	}
	if err := fs.Chdir("/"); err != nil {
		m.logBootError("", err)
		return
	}
	if err := checkUnitDirectories(links); err != nil {
		if !errors.Is(err, filesystem.ErrNotExist) {
			m.logBootError("", err)
		}
		return
	}
	if err := migrateEnabledUnits(fs, links); err != nil {
		m.logBootError("", err)
	}
	if err := checkSystemPath(links, bootWantsDir, true); err != nil {
		if !errors.Is(err, filesystem.ErrNotExist) {
			m.logBootError("", err)
		}
		return
	}
	entries, err := fs.List(bootWantsDir)
	if err != nil {
		m.logBootError("", err)
		return
	}
	slices.SortFunc(entries, func(a, b filesystem.Info) int {
		return strings.Compare(a.Name, b.Name)
	})
	control := m.Control(fs)
	for _, entry := range entries {
		name := entry.Name
		if entry.Mode&filesystem.ModeSymlink == 0 || !validServiceName(name) {
			continue
		}
		err := checkEnabledLink(links, name)
		if err == nil {
			err = checkSystemPath(links, path.Join(vfs.SystemUnitDir, name), false)
		}
		if err == nil {
			err = control.Start(name)
		}
		if err != nil {
			m.logBootError(name, fmt.Errorf("start %q: %w", name, err))
		}
	}
}

func (m *Manager) logBootError(name string, err error) {
	// Boot has no foreground stderr reader. Record failures in the journal.
	_ = m.journal.write(journalEntry{Unit: name, PID: m.table.Init().PID, Stderr: true},
		[]byte(fmt.Sprintf("systemd: %v\n", err)))
}

func migrateEnabledUnits(fs filesystem.FS, links filesystem.SymlinkFS) error {
	if err := checkSystemPath(links, legacyEnabledPath, false); err != nil {
		if errors.Is(err, filesystem.ErrNotExist) {
			return nil
		}
		return err
	}
	data, err := fs.Read(legacyEnabledPath)
	if err != nil {
		return err
	}
	for name := range strings.SplitSeq(string(data), "\n") {
		if name != "" && !validServiceName(name) {
			return fmt.Errorf("%w %q", ErrInvalidServiceName, name)
		}
	}
	if len(data) > 0 {
		if err := ensureWantsDir(fs, links); err != nil {
			return err
		}
		for name := range strings.SplitSeq(string(data), "\n") {
			if name != "" {
				if err := linkEnabledUnit(links, name); err != nil {
					return err
				}
			}
		}
	}
	return fs.Remove(legacyEnabledPath, false)
}

func ensureWantsDir(fs filesystem.FS, links filesystem.SymlinkFS) error {
	if err := fs.Mkdir(bootWantsDir); err != nil && !errors.Is(err, filesystem.ErrExists) {
		return err
	}
	return checkSystemPath(links, bootWantsDir, true)
}

func linkEnabledUnit(links filesystem.SymlinkFS, name string) error {
	err := links.Symlink("../"+name, path.Join(bootWantsDir, name))
	if errors.Is(err, filesystem.ErrExists) {
		return checkEnabledLink(links, name)
	}
	return err
}

func checkEnabledLink(links filesystem.SymlinkFS, name string) error {
	filename := path.Join(bootWantsDir, name)
	target, err := links.Readlink(filename)
	if err != nil {
		return err
	}
	if target != "../"+name && target != path.Join(vfs.SystemUnitDir, name) {
		return fmt.Errorf("%s: %w", filename, filesystem.ErrInvalid)
	}
	return nil
}

func unitFileState(fs filesystem.FS, name string) string {
	service, err := LoadService(fs, name)
	if err != nil {
		return "unknown"
	}
	if links, ok := fs.(filesystem.SymlinkFS); ok {
		err := checkSystemPath(links, bootWantsDir, true)
		if err == nil {
			err = checkEnabledLink(links, name)
		}
		if err == nil {
			if checkUnitDirectories(links) != nil || checkSystemPath(links, path.Join(vfs.SystemUnitDir, name), false) != nil {
				return "unknown"
			}
			return "enabled"
		}
		if !errors.Is(err, filesystem.ErrNotExist) && !errors.Is(err, filesystem.ErrNotImplemented) {
			return "unknown"
		}
	}
	if slices.Contains(service.WantedBy, bootTarget) {
		return "disabled"
	}
	return "static"
}

func checkUnitDirectories(links filesystem.SymlinkFS) error {
	for _, dir := range []string{"/", "/etc", path.Dir(vfs.SystemUnitDir), vfs.SystemUnitDir} {
		if err := checkSystemPath(links, dir, true); err != nil {
			return err
		}
	}
	return nil
}

func checkSystemPath(links filesystem.SymlinkFS, filename string, directory bool) error {
	info, err := links.Lstat(filename)
	if err != nil {
		return fmt.Errorf("%s: %w", filename, err)
	}
	if info.IsDir() != directory || info.Mode&(filesystem.ModeCharDevice|filesystem.ModeSymlink) != 0 ||
		info.Owner.Name != user.RootName || info.Mode&0022 != 0 {
		return fmt.Errorf("%s: %w", filename, filesystem.ErrPermission)
	}
	return nil
}
