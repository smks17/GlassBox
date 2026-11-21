package systemd

import (
	"fmt"
	"path"
	"slices"
	"strings"

	"glass/internal/engine/filesystem"
	"glass/internal/engine/vfs"
)

type Service struct {
	Description string
	ExecStart   string
	WantedBy    []string
}

// LoadService reads a named service from the simulated system unit directory.
func LoadService(fs filesystem.FS, name string) (Service, error) {
	if !validServiceName(name) {
		return Service{}, fmt.Errorf("%w %q", ErrInvalidServiceName, name)
	}
	filename := path.Join(vfs.SystemUnitDir, name)
	data, err := fs.Read(filename)
	if err != nil {
		return Service{}, fmt.Errorf("read %q: %w", filename, err)
	}
	service, err := ParseService(string(data))
	if err != nil {
		return Service{}, fmt.Errorf("parse %q: %w", filename, err)
	}
	return service, nil
}

func validServiceName(name string) bool {
	return path.Base(name) == name && strings.HasSuffix(name, ".service") && name != ".service" &&
		!strings.ContainsAny(name, "\x00\r\n")
}

type UnitFile struct {
	Name  string
	State string
}

// ListUnitFiles returns installed service files and their enablement states in alphabetical order.
func ListUnitFiles(fs filesystem.FS) ([]UnitFile, error) {
	entries, err := fs.List(vfs.SystemUnitDir)
	if err != nil {
		return nil, err
	}
	var files []UnitFile
	for _, entry := range entries {
		if entry.Mode&(filesystem.ModeDirectory|filesystem.ModeCharDevice) == 0 && validServiceName(entry.Name) {
			files = append(files, UnitFile{Name: entry.Name, State: unitFileState(fs, entry.Name)})
		}
	}
	slices.SortFunc(files, func(a, b UnitFile) int { return strings.Compare(a.Name, b.Name) })
	return files, nil
}

// ParseService reads the unit, service, and installation settings supported by the simulator.
func ParseService(text string) (Service, error) {
	var service Service
	var section string
	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = line[1 : len(line)-1]
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		switch {
		case section == "Unit" && key == "Description":
			service.Description = strings.TrimSpace(value)
		case section == "Service" && key == "ExecStart":
			service.ExecStart = strings.TrimSpace(value)
		case section == "Install" && key == "WantedBy":
			targets := strings.Fields(value)
			if len(targets) == 0 {
				service.WantedBy = nil
			} else {
				service.WantedBy = append(service.WantedBy, targets...)
			}
		}
	}
	if service.ExecStart == "" {
		return Service{}, ErrMissingExecStart
	}
	return service, nil
}
