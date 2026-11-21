package systemd

import (
	"path"

	"glass/internal/engine/filesystem"
	"glass/internal/engine/proc"
	"glass/internal/engine/vfs"
)

type ServiceControl interface {
	Start(name string) error
	Stop(name string) error
	Restart(name string) error
	Enable(name string) error
	Disable(name string) error
	Status(name string) (ServiceStatus, error)
	ListUnits() []ServiceStatus

	// ReadJournal returns the last lineLimit lines. A negative limit returns all retained output.
	ReadJournal(name string, lineLimit int) (string, error)
}

type ServiceStatus struct {
	Name        string
	Description string
	Path        string
	MainPID     proc.PID
}

func FileStatus(fs filesystem.FS, name string) (ServiceStatus, error) {
	service, err := LoadService(fs, name)
	if err != nil {
		return ServiceStatus{}, err
	}
	return service.Status(name), nil
}

func (s Service) Status(name string) ServiceStatus {
	return ServiceStatus{Name: name, Description: s.Description, Path: path.Join(vfs.SystemUnitDir, name)}
}
