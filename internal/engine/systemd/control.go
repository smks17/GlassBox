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
	Status(name string) (ServiceStatus, error)
}

type ServiceStatus struct {
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
	return ServiceStatus{Description: s.Description, Path: path.Join(vfs.SystemUnitDir, name)}
}
