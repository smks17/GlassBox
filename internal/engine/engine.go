package engine

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/tetratelabs/wazero"

	"glass/internal/engine/clock"
	"glass/internal/engine/commands"
	"glass/internal/engine/dev"
	"glass/internal/engine/filesystem"
	"glass/internal/engine/network"
	"glass/internal/engine/shell"
	"glass/internal/engine/stream"
	"glass/internal/engine/user"
	"glass/internal/engine/vfs"
)

const ShellName = "glass"

// App preserves the single-machine API used by the CLI and browser.
type App struct{ *Machine }

func NewApp() *App {
	return NewAppOn(vfs.New())
}

// Network is the machine's network, for the host to plug interfaces into.
func (app *Machine) Network() *network.NetworkManager {
	app.mu.Lock()
	defer app.mu.Unlock()
	return app.network
}

// SetYield installs the host's hook for giving its event loop a turn.
func (app *Machine) SetYield(tick func()) {
	app.op.Lock()
	defer app.op.Unlock()
	app.tick = tick
	app.session.shell.SetYield(tick)
}

func (app *Machine) SetPasswordReader(read func(context.Context, string) (string, error)) {
	app.op.Lock()
	defer app.op.Unlock()
	app.readPassword = read
	app.session.shell.ReadPassword = read
}

func (app *Machine) SetBackgroundOutput(out, errOut io.Writer) {
	app.op.Lock()
	defer app.op.Unlock()
	app.background = output{out, errOut}
	app.session.background = app.background
	app.session.setOutput(out, errOut)
}

// Suspend is Ctrl-Z: it stops the command running now and keeps it as a job.
// The host calls it from another goroutine, while ExecuteStream runs.
func (app *Machine) Suspend() {
	app.mu.Lock()
	sh := app.session.shell
	app.mu.Unlock()
	sh.Suspend()
}

// NewAppOn starts a shell on a filesystem of your choosing
func NewAppOn(fsys filesystem.FS) *App {
	m := newMachine("default", fsys)
	m.ownedCache = wazero.NewCompilationCache()
	m.SetWasiCache(m.ownedCache)
	_ = m.Start()
	return &App{Machine: m}
}

func (app *Machine) Snapshot() ([]byte, error) {
	app.op.Lock()
	defer app.op.Unlock()
	return app.session.Snapshot()
}

func (app *Machine) Restore(data []byte) error {
	// Loading is side-effect free; a rejected snapshot leaves this boot intact.
	session, err := LoadSession(data)
	if err != nil {
		return err
	}
	app.op.Lock()
	defer app.op.Unlock()
	app.mu.Lock()
	if app.state != MachineRunning && app.state != MachineStopped {
		app.mu.Unlock()
		session.Close()
		return ErrMachineNotRunning
	}
	running := app.state == MachineRunning
	old, network := app.session, app.network
	app.mu.Unlock()
	old.stdout.Close()
	old.stderr.Close()
	// Network reads may not observe process cancellation; release descriptors
	// before waiting for their workers.
	if network != nil {
		network.Close()
	}
	old.Close()
	app.mu.Lock()
	app.session = session
	if running {
		app.wireBoot()
	}
	app.mu.Unlock()
	return nil
}

// Session is one shell and the filesystem it runs on.
type Session struct {
	clock     *clock.Clock
	fs        filesystem.FS
	shell     *shell.Shell
	stdout    *stream.Buffer
	stderr    *stream.Buffer
	sudoMutex sync.Mutex
	// TODO: Later add last login for each user and check based on that
	sudoExpires    map[string]time.Time
	sudoGeneration uint64

	background output // where output goes between lines; nil keeps it waiting
}

type output struct{ out, errOut io.Writer }

func (s *Session) Close() {
	s.stdout.Close()
	s.stderr.Close()
	s.shell.Close()
}

func (s *Session) setOutput(out, errOut io.Writer) {
	s.stdout.SetOutput(out)
	s.stderr.SetOutput(errOut)
}

func NewSession() *Session {
	c := clock.New()
	return newSession(vfs.NewWithClock(c), c)
}

func newSession(filesystem *vfs.VFS, c *clock.Clock) *Session {
	s := &Session{clock: c, fs: filesystem, shell: shell.NewWithClock(filesystem, nil, c), stdout: stream.NewBuffer(), stderr: stream.NewBuffer()}
	s.shell.SwitchUser = s.SwitchUser
	s.shell.AuthorizeSudo = s.authorizeSudo
	_ = filesystem.MakeDevice("/dev/tty", dev.NewTerminal(strings.NewReader(""), s.stdout), 0666)
	resident, err := filesystem.UsersDB().Resident()
	if err != nil || s.SetUser(resident.Name) != nil {
		s.SetUser(user.RootName)
	}
	return s
}

func (s *Session) tree() (*vfs.VFS, bool) {
	tree, inMemory := s.fs.(*vfs.VFS)
	return tree, inMemory
}

func (s *Session) SetUser(name string) error {
	tree, inMemory := s.tree()
	if !inMemory {
		return ErrRealFilesystem
	}
	id, err := tree.UsersDB().Lookup(name)
	if err != nil {
		return err
	}
	s.sudoMutex.Lock()
	s.sudoExpires = nil
	s.sudoGeneration++
	s.sudoMutex.Unlock()
	tree.SetIdentity(id)
	s.shell.SetUser(id)
	vars := s.shell.Vars()
	vars[commands.EnvUser] = id.Name
	vars[commands.EnvHome] = id.Home
	return nil
}

func (app *Machine) Login(name, password string) error {
	if !app.running() {
		return ErrMachineNotRunning
	}
	app.op.Lock()
	defer app.op.Unlock()
	if !app.running() {
		return ErrMachineNotRunning
	}
	return app.session.Login(name, password)
}

func (s *Session) Login(name, password string) error {
	tree, inMemory := s.tree()
	if !inMemory {
		return ErrRealFilesystem
	}
	if err := tree.UsersDB().Authenticate(name, password); err != nil {
		return err
	}
	if err := s.SetUser(name); err != nil {
		return err
	}
	if err := s.fs.Chdir(s.fs.Identity().Home); err != nil {
		s.fs.Chdir("/")
	}
	return nil
}

func (s *Session) SwitchUser(name, password string) error {
	tree, inMemory := s.tree()
	if !inMemory {
		return ErrRealFilesystem
	}
	if !s.fs.Identity().IsRoot() {
		if err := tree.UsersDB().Authenticate(name, password); err != nil {
			return err
		}
	}
	return s.SetUser(name)
}

func (s *Session) User() string { return s.fs.Identity().Name }

// at is the wall clock the shell believes in — TZ and `date -s` included.
func (s *Session) at() string { return s.clock.In(s.shell.Vars()["TZ"]).Format("15:04:05") }

// Now is that clock, for callers outside a command's result.
func (app *Machine) Now() string {
	app.op.Lock()
	defer app.op.Unlock()
	return app.session.at()
}

// Result is what one line of input produced.
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int    // Unix convention: 0 is success, non-zero is failure
	Cwd      string // lets the UI render the prompt without a second call
	Exited   bool   // the script asked the shell to close
	User     string // ...and who it belongs to, so `su` is visible in it
	At       string // the shell clock when it ran, for the UI to stamp the line

	Interrupted bool
}

// Execute runs one line of input and collects everything it wrote.
func (app *Machine) Execute(line string) Result {
	return app.ExecuteStream(context.Background(), line, nil, nil)
}

func (app *Machine) ExecuteStream(ctx context.Context, line string, out, errOut io.Writer) Result {
	if !app.running() {
		return Result{ExitCode: 1, Stderr: ErrMachineNotRunning.Error() + "\n"}
	}
	app.op.Lock()
	defer app.op.Unlock()
	if !app.running() {
		return app.notRunning()
	}
	ctx, cancel := app.executionContext(ctx)
	defer cancel()
	session := app.session
	if strings.TrimSpace(line) == "" {
		return session.result(0, Result{})
	}
	session.shell.History = append(session.shell.History, commands.HistoryEntry{Line: line, At: session.clock.Now()})
	return session.runStream(ctx, line, out, errOut)
}

// RunScript runs a whole script, with args as $1 onwards.
func (app *Machine) RunScript(src string, args []string) Result {
	return app.RunScriptStream(context.Background(), src, args, nil, nil)
}

// RunScriptStream is RunScript with the streaming and cancellation that
// ExecuteStream has.
func (app *Machine) RunScriptStream(ctx context.Context, src string, args []string, out, errOut io.Writer) Result {
	if !app.running() {
		return Result{ExitCode: 1, Stderr: ErrMachineNotRunning.Error() + "\n"}
	}
	app.op.Lock()
	defer app.op.Unlock()
	if !app.running() {
		return app.notRunning()
	}
	ctx, cancel := app.executionContext(ctx)
	defer cancel()
	app.session.shell.SetParams(args)
	return app.session.runStream(ctx, src, out, errOut)
}

// result stamps the session's own state onto a Result the caller has
// already filled in the interesting parts of.
func (s *Session) result(status int, r Result) Result {
	r.ExitCode = status
	r.Cwd = s.fs.Cwd()
	r.User = s.User()
	r.At = s.at()
	return r
}

func (s *Session) runStream(ctx context.Context, src string, out, errOut io.Writer) Result {
	var stdout, stderr bytes.Buffer

	outW, errW := io.Writer(&stdout), io.Writer(&stderr)
	buffered := out == nil && errOut == nil
	if !buffered {
		outW, errW = io.Discard, io.Discard
		if out != nil {
			outW = out
		}
		if errOut != nil {
			errW = errOut
		}
	}

	s.setOutput(outW, errW)
	fds := stream.NewFDTable(stream.NewStreamFile(strings.NewReader(""), nil), s.stdout.WriteEnd(), s.stderr.WriteEnd())
	s.shell.ReportFinishedJobs(fds.Stderr())

	s.shell.SetContext(ctx)
	defer s.shell.SetContext(context.Background())

	status := s.shell.Run(src, fds)
	s.setOutput(s.background.out, s.background.errOut)
	code, exited := s.shell.Exiting()
	if exited {
		status = code
	}
	interrupted := s.shell.Interrupted() || ctx.Err() != nil
	if interrupted {
		status = 130
	}

	result := Result{Exited: exited, Interrupted: interrupted}
	if buffered {
		result.Stdout = stdout.String()
		result.Stderr = stderr.String()
	}
	return s.result(status, result)
}

// Upload writes a file into the session's filesystem.
func (app *Machine) Upload(path string, data []byte) Result {
	if !app.running() {
		return Result{ExitCode: 1, Stderr: ErrMachineNotRunning.Error() + "\n"}
	}
	app.op.Lock()
	defer app.op.Unlock()
	if !app.running() {
		return app.notRunning()
	}
	if err := app.session.fs.Create(path); err != nil {
		return Result{Stderr: fmt.Sprintf("upload: %s: %v\n", path, err), ExitCode: 1, Cwd: app.session.fs.Cwd(), User: app.session.User(), At: app.session.at()}
	}
	if err := app.session.fs.Write(path, data, false); err != nil {
		return Result{Stderr: fmt.Sprintf("upload: %s: %v\n", path, err), ExitCode: 1, Cwd: app.session.fs.Cwd(), User: app.session.User(), At: app.session.at()}
	}
	return Result{ExitCode: 0, Cwd: app.session.fs.Cwd(), User: app.session.User(), At: app.session.at()}
}

func (app *Machine) Cwd() string {
	app.op.Lock()
	defer app.op.Unlock()
	return app.session.Cwd()
}

func (s *Session) Cwd() string { return s.fs.Cwd() }

func (app *Machine) User() string {
	app.op.Lock()
	defer app.op.Unlock()
	return app.session.User()
}

// Complete offers the names in the working directory that extend a prefix.
func (app *Machine) Complete(prefix string) []string {
	app.op.Lock()
	defer app.op.Unlock()
	return app.session.Complete(prefix)
}

func (s *Session) Complete(prefix string) []string {
	entries, err := s.fs.List(s.fs.Cwd())
	if err != nil {
		return nil
	}

	var matches []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name, ".") || !strings.HasPrefix(entry.Name, prefix) {
			continue
		}
		matches = append(matches, entry.Name)
	}
	return matches
}
