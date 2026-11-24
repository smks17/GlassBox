package engine

import (
	"context"
	"errors"
	"strings"
	"sync"

	"glass/internal/engine/clock"
	"glass/internal/engine/commands"
	"glass/internal/engine/filesystem"
	"glass/internal/engine/network"
	"glass/internal/engine/shell"
	"glass/internal/engine/stream"
	"glass/internal/engine/systemd"
	"glass/internal/engine/user"
	"glass/internal/engine/vfs"
	"glass/internal/engine/wasi"
)

type MachineState string

const (
	MachineStopped  MachineState = "stopped"
	MachineStarting MachineState = "starting"
	MachineRunning  MachineState = "running"
	MachineStopping MachineState = "stopping"
)

var ErrMachineNotRunning = errors.New("machine is not running")
var ErrMachineStopping = errors.New("machine is stopping")

type MachineInfo struct {
	ID    string
	State MachineState
}

type Machine struct {
	op           sync.Mutex
	mu           sync.Mutex
	id           string
	state        MachineState
	ctx          context.Context
	cancel       context.CancelFunc
	stopped      chan struct{}
	session      *Session
	network      *network.NetworkManager
	tick         func()
	background   output
	wasiCache    wasi.Cache
	ownedCache   wasi.Cache
	readPassword func(context.Context, string) (string, error)
}

func NewMachine(id string) (*Machine, error) {
	if strings.TrimSpace(id) == "" {
		return nil, errors.New("machine ID must not be empty")
	}
	return newMachine(id, vfs.New()), nil
}

func (m *Machine) SetWasiCache(cache wasi.Cache) {
	m.op.Lock()
	defer m.op.Unlock()
	m.wasiCache = cache
	m.session.shell.WasiCache = cache
}

func newMachine(id string, fs filesystem.FS) *Machine {
	return &Machine{id: id, state: MachineStopped, session: sessionOn(fs, nil)}
}

// Close stops the machine and releases its default compilation cache.
func (m *Machine) Close(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := m.Stop(ctx); err != nil {
		return err
	}
	if m.ownedCache != nil {
		return m.ownedCache.Close(ctx)
	}
	return nil
}

func sessionOn(fs filesystem.FS, c *clock.Clock) *Session {
	if tree, ok := fs.(*vfs.VFS); ok {
		s := newSession(tree, tree.Clock())
		if err := fs.Chdir(fs.Identity().Home); err != nil {
			_ = fs.Chdir("/")
		}
		return s
	}
	if c == nil {
		c = clock.New()
	}
	s := &Session{clock: c, fs: fs, shell: shell.NewWithClock(fs, nil, c), stdout: stream.NewBuffer(), stderr: stream.NewBuffer()}
	s.shell.SwitchUser = s.SwitchUser
	id := fs.Identity()
	s.shell.Vars()[commands.EnvUser], s.shell.Vars()[commands.EnvHome] = id.Name, id.Home
	return s
}

func (m *Machine) Info() MachineInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	return MachineInfo{ID: m.id, State: m.state}
}

func (m *Machine) Start() error {
	switch m.Info().State {
	case MachineRunning:
		return nil
	case MachineStopping:
		return ErrMachineStopping
	}
	m.op.Lock()
	defer m.op.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	switch m.state {
	case MachineRunning:
		return nil
	case MachineStopping:
		return ErrMachineStopping
	}
	m.state = MachineStarting
	m.session.Close()
	m.session = sessionOn(m.session.fs, m.session.clock)
	m.ctx, m.cancel = context.WithCancel(context.Background())
	m.stopped = make(chan struct{})
	m.wireBoot(true)
	m.state = MachineRunning
	return nil
}

// wireBoot installs boot-scoped resources; caller holds op and mu.
func (m *Machine) wireBoot(startServices bool) {
	m.session.shell.SetLifetime(m.ctx)
	m.session.shell.SetYield(m.tick)
	m.session.shell.WasiCache = m.wasiCache
	m.session.shell.ReadPassword = m.readPassword
	m.session.background = m.background
	m.session.setOutput(m.background.out, m.background.errOut)
	if tree, ok := m.session.tree(); ok {
		if sys, err := tree.Resolve("/sys/class/net"); err == nil {
			for _, e := range sys.Entries() {
				_ = sys.Unlink(e.Name)
			}
		}
	}
	m.network = network.NewNetworkManager(m.ctx, &m.session.fs)
	m.network.Start()
	m.session.shell.Network = m.network
	m.seedSys()
	if tree, ok := m.session.tree(); ok {
		m.wireServices(tree, startServices)
	}
}

func (m *Machine) wireServices(tree *vfs.VFS, startServices bool) {
	sh := m.session.shell
	systemFS := tree.Fork().(*vfs.VFS)
	systemFS.SetIdentity(user.Identity{
		Name: user.RootName, UID: user.RootUID, Groups: []user.Group{{Name: user.RootGroup}},
	})
	sh.Services = systemd.NewManager(sh.Processes(), sh.PrepareServiceCommand, systemFS)
	if startServices {
		sh.Services.StartEnabled(systemFS)
	}
}

func (m *Machine) Stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	m.mu.Lock()
	if m.state == MachineStopped {
		m.mu.Unlock()
		return nil
	}
	if m.state != MachineStopping {
		m.state = MachineStopping
		m.cancel()
		s := m.session
		go func() {
			// Release this session's blocked writers before waiting for op.
			// Restore may replace m.session while cleanup waits.
			s.stdout.Close()
			s.stderr.Close()
			m.finishStop()
		}()
	}
	done := m.stopped
	m.mu.Unlock()
	select {
	case <-done:
		return nil
	default:
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *Machine) finishStop() {
	m.op.Lock()
	defer m.op.Unlock()
	m.session.Close()
	m.network.Close()
	m.mu.Lock()
	m.state = MachineStopped
	close(m.stopped)
	m.mu.Unlock()
}

func (m *Machine) running() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state == MachineRunning
}

func (m *Machine) notRunning() Result {
	return m.session.result(1, Result{Stderr: ErrMachineNotRunning.Error() + "\n"})
}

func (m *Machine) executionContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	stop := context.AfterFunc(m.ctx, cancel)
	if m.ctx.Err() != nil {
		cancel()
	}
	return ctx, func() { stop(); cancel() }
}
