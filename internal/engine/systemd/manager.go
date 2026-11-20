package systemd

import (
	"context"
	"errors"
	"slices"
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
	journal journal
}

type runningService struct {
	group  *proc.Group
	status ServiceStatus
}

// PrepareCommand resolves ExecStart before the manager creates its process.
type PrepareCommand func(filesystem.FS, []string) (func(*proc.Process, *stream.FDTable) int, error)

func NewManager(table *proc.Table, prepare PrepareCommand, journalFS filesystem.FS) *Manager {
	table.Init().SetExec("systemd", nil)
	manager := &Manager{
		table: table, active: make(map[string]runningService), prepare: prepare,
		journal: journal{fs: journalFS},
	}
	manager.journal.initErr = manager.journal.initialize()
	return manager
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

func (m *control) ReadJournal(name string, lineLimit int) (string, error) {
	return m.journal.read(name, lineLimit)
}

func (m *control) Status(name string) (ServiceStatus, error) {
	m.mu.Lock()
	service, running := m.runningLocked(name)
	m.mu.Unlock()
	if !running {
		return FileStatus(m.fs, name)
	}
	if err := m.checkUnitRead(service.status.Path); err != nil {
		return ServiceStatus{}, err
	}
	return service.status, nil
}

func (m *control) ListUnits() []ServiceStatus {
	m.mu.Lock()
	var statuses []ServiceStatus
	for name := range m.active {
		if service, running := m.runningLocked(name); running {
			statuses = append(statuses, service.status)
		}
	}
	m.mu.Unlock()

	statuses = slices.DeleteFunc(statuses, func(status ServiceStatus) bool {
		return m.checkUnitRead(status.Path) != nil
	})
	slices.SortFunc(statuses, func(a, b ServiceStatus) int {
		return strings.Compare(a.Name, b.Name)
	})
	return statuses
}

func (m *control) checkUnitRead(path string) error {
	// Keep read permissions effective even when reporting the loaded definition.
	file, err := m.fs.OpenDefault(path)
	if err == nil {
		file.Close()
	} else if !errors.Is(err, filesystem.ErrNotExist) {
		return err
	}
	return nil
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
			stdout := journalWriter{journal: &m.journal, entry: journalEntry{Unit: name, PID: p.PID}}
			stderr := journalWriter{journal: &m.journal, entry: journalEntry{Unit: name, PID: p.PID, Stderr: true}}
			fds := stream.NewStdTable(strings.NewReader(""), stdout, stderr)
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

func (m *control) Restart(name string) error {
	if err := m.Stop(name); err != nil {
		return err
	}
	return m.Start(name)
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
