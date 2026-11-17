package systemd

import (
	"fmt"
	"path"
	"strings"

	"glass/internal/engine/filesystem"
	"glass/internal/engine/vfs"
)

type Service struct {
	Description string
	ExecStart   string
}

// LoadService reads a named service from the simulated system unit directory.
func LoadService(fs filesystem.FS, name string) (Service, error) {
	if path.Base(name) != name || !strings.HasSuffix(name, ".service") || name == ".service" {
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

// ParseService reads the two fields supported by the simulator.
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
		}
	}
	if service.ExecStart == "" {
		return Service{}, ErrMissingExecStart
	}
	return service, nil
}
