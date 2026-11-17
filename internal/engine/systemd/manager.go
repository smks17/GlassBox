package systemd

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"time"

	"glass/internal/engine/filesystem"
	"glass/internal/engine/proc"
	"glass/internal/engine/stream"
)

// Manager tracks manually started services for one machine boot.
type Manager struct {
	mu      sync.Mutex
	table   *proc.Table
	active  map[string]runningService
	prepare PrepareCommand
}

type runningService struct {
	group  *proc.Group
	status ServiceStatus
}

// PrepareCommand resolves ExecStart before the manager creates its process.
type PrepareCommand func(filesystem.FS, []string) (func(*proc.Process, *stream.FDTable) int, error)

func NewManager(table *proc.Table, prepare PrepareCommand) *Manager {
	table.Init().SetExec("systemd", nil)
	return &Manager{table: table, active: make(map[string]runningService), prepare: prepare}
}

func (m *Manager) runningLocked(name string) (runningService, bool) {
	service, ok := m.active[name]
	return service, ok && len(service.group.List()) > 0
}

type control struct {
	*Manager
	fs filesystem.FS
}

func (m *Manager) Control(fs filesystem.FS) ServiceControl {
	return &control{Manager: m, fs: fs}
}

func (m *control) Status(name string) (ServiceStatus, error) {
	m.mu.Lock()
	service, running := m.runningLocked(name)
	m.mu.Unlock()
	if !running {
		return FileStatus(m.fs, name)
	}
	// Keep read permissions effective even when reporting the loaded definition.
	file, err := m.fs.OpenDefault(service.status.Path)
	if err == nil {
		file.Close()
	} else if !errors.Is(err, filesystem.ErrNotExist) {
		return ServiceStatus{}, err
	}
	return service.status, nil
}

func (m *control) Start(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, running := m.runningLocked(name); running {
		return nil
	}
	service, err := LoadService(m.fs, name)
	if err != nil {
		return err
	}
	words := strings.Fields(service.ExecStart)
	run, err := m.prepare(m.fs, words)
	if err != nil {
		return err
	}
	group := m.table.NewGroup()
	p := group.Spawn(proc.Spec{
		Parent: m.table.Init(), User: m.fs.Identity(), Name: words[0], Args: words[1:],
		Run: func(p *proc.Process) int {
			fds := stream.NewStdTable(strings.NewReader(""), io.Discard, io.Discard)
			defer fds.Destroy()
			return run(p, fds)
		},
	})
	status := service.Status(name)
	status.MainPID = p.PID
	m.active[name] = runningService{group: group, status: status}
	go func() {
		processes := []*proc.Process{p}
		m.table.Wait(processes, false)
		m.table.Reap(processes)
		group.Wait(false)
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.active[name].group == group {
			delete(m.active, name)
		}
	}()
	return nil
}

func (m *control) Stop(name string) error {
	m.mu.Lock()
	service, running := m.runningLocked(name)
	m.mu.Unlock()
	if !running {
		_, err := LoadService(m.fs, name)
		return err
	}
	return m.stopGroup(service.group, 90*time.Second)
}

func (m *Manager) stopGroup(group *proc.Group, timeout time.Duration) error {
	init := m.table.Init()
	if err := group.Kill(init, proc.SIGTERM); err != nil {
		if errors.Is(err, proc.ErrNoProcess) {
			return nil
		}
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if group.WaitEmpty(ctx) == nil {
		return nil
	}
	if err := group.Kill(init, proc.SIGKILL); err != nil && !errors.Is(err, proc.ErrNoProcess) {
		return err
	}
	group.Wait(false)
	return nil
}
