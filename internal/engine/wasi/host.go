package wasi

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"

	"glass/internal/engine/clock"
	"glass/internal/engine/filesystem"
	"glass/internal/engine/stream"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

// The file types fd_fdstat_get and fd_filestat_get report.
const (
	filetypeCharDevice = 2
	filetypeDirectory  = 3
	filetypeRegular    = 4
)

const (
	oflagCreat     = 1 << 0
	oflagDirectory = 1 << 1
	oflagExcl      = 1 << 2
	oflagTrunc     = 1 << 3

	rightFdRead  = 1 << 1
	rightFdWrite = 1 << 6

	fdflagAppend = 1 << 0
)

type openFile struct {
	path string
	file *stream.File
}

type hostWasi struct {
	cfg   WasiContext
	files map[uint32]*openFile
	next  uint32
}

func (h *hostWasi) closeAll() {
	for _, f := range h.files {
		if f.file != nil {
			f.file.Close()
		}
	}
}

func (h *hostWasi) instantiateHost(ctx context.Context, runtime wazero.Runtime, compiled wazero.CompiledModule) error {
	const module = "wasi_snapshot_preview1"
	calls := map[string]any{
		// The runner has no other guest threads to schedule, so yielding succeeds immediately.
		"sched_yield":           func() uint32 { return errnoSuccess },
		"args_sizes_get":        h.argsSizesGet,
		"args_get":              h.argsGet,
		"environ_sizes_get":     h.environSizesGet,
		"environ_get":           h.environGet,
		"clock_time_get":        h.clockTimeGet,
		"random_get":            h.randomGet,
		"fd_write":              h.fdWrite,
		"path_open":             h.pathOpen,
		"fd_read":               h.fdRead,
		"fd_close":              h.fdClose,
		"fd_seek":               h.fdSeek,
		"fd_fdstat_get":         h.fdFdstatGet,
		"fd_prestat_get":        h.fdPrestatGet,
		"fd_prestat_dir_name":   h.fdPrestatDirName,
		"fd_filestat_get":       h.fdFilestatGet,
		"fd_fdstat_set_flags":   h.fdFdstatSetFlags,
		"path_create_directory": h.pathCreateDirectory,
		"path_filestat_get":     h.pathFilestatGet,
		"path_link":             h.pathLink,
		"path_readlink":         h.pathReadlink,
		"path_remove_directory": h.pathRemoveDirectory,
		"path_rename":           h.pathRename,
		"path_symlink":          h.pathSymlink,
		"path_unlink_file":      h.pathUnlinkFile,
	}

	builder := runtime.NewHostModuleBuilder(module)
	for name, fn := range calls {
		builder.NewFunctionBuilder().WithFunc(fn).Export(name)
	}
	builder.NewFunctionBuilder().WithGoModuleFunction(api.GoModuleFunc(procExit), []api.ValueType{api.ValueTypeI32}, nil).Export("proc_exit")

	for _, def := range compiled.ImportedFunctions() {
		mod, name, _ := def.Import()
		if _, ok := calls[name]; mod != module || ok || name == "proc_exit" {
			continue
		}
		builder.NewFunctionBuilder().
			WithGoModuleFunction(h.enosys(name), def.ParamTypes(), def.ResultTypes()).
			Export(name)
	}

	_, err := builder.Instantiate(ctx)
	return err
}

func (h *hostWasi) enosys(name string) api.GoModuleFunc {
	return func(_ context.Context, _ api.Module, stack []uint64) {
		fmt.Fprintf(h.cfg.Stderr, "wasi: %s not implemented\n", name)
		stack[0] = errnoNosys
	}
}

func (h *hostWasi) reader(fd uint32) io.Reader {
	if fd == 0 {
		return h.cfg.Stdin
	}
	if f, ok := h.files[fd]; ok && f.file != nil {
		return f.file
	}
	return nil
}

func (h *hostWasi) writer(fd uint32) io.Writer {
	switch fd {
	case 1:
		return h.cfg.Stdout
	case 2:
		return h.cfg.Stderr
	}
	// A file opened read-only still lands here; its own Write refuses it.
	if f, ok := h.files[fd]; ok && f.file != nil {
		return f.file
	}
	return nil
}

func (h *hostWasi) stream(fd uint32) (*stream.File, uint32) {
	if fd <= 2 {
		return nil, errnoSpipe
	}
	f, ok := h.files[fd]
	if !ok {
		return nil, errnoBadf
	}
	if f.file == nil {
		return nil, errnoIsDir
	}
	return f.file, errnoSuccess
}

func (h *hostWasi) environ() []string {
	env := make([]string, 0, len(h.cfg.Env)+1)
	for k, v := range h.cfg.Env {
		if k != "PWD" {
			env = append(env, k+"="+v)
		}
	}
	env = append(env, "PWD="+h.cfg.FS.Cwd())
	sort.Strings(env)
	return env
}

func (h *hostWasi) argsGet(_ context.Context, m api.Module, ptrs, buf uint32) uint32 {
	return strings(m, h.cfg.Args, ptrs, buf)
}

func (h *hostWasi) fdWrite(_ context.Context, m api.Module, fd, iovs, iovsLen, nPtr uint32) uint32 {
	w := h.writer(fd)
	if w == nil {
		return errnoBadf
	}
	vecs, ok := iovecs(m, iovs, iovsLen)
	if !ok {
		return errnoInval
	}
	total := 0
	for _, buf := range vecs {
		n, err := w.Write(buf)
		total += n
		if err != nil {
			// A broken pipe
			return errnoIO
		}
	}
	m.Memory().WriteUint32Le(nPtr, uint32(total))
	return errnoSuccess
}

func (h *hostWasi) fdRead(_ context.Context, m api.Module, fd, iovs, iovsLen, nPtr uint32) uint32 {
	r := h.reader(fd)
	if r == nil {
		return errnoBadf
	}
	vecs, ok := iovecs(m, iovs, iovsLen)
	if !ok {
		return errnoInval
	}
	total := 0
	for _, buf := range vecs {
		n, err := r.Read(buf)
		total += n
		if err != nil && !errors.Is(err, io.EOF) {
			return errnoIO
		}
		if n < len(buf) {
			break
		}
	}
	m.Memory().WriteUint32Le(nPtr, uint32(total))
	return errnoSuccess
}

func (h *hostWasi) fdClose(_ context.Context, _ api.Module, fd uint32) uint32 {
	f, ok := h.files[fd]
	if !ok {
		return errnoBadf
	}
	delete(h.files, fd)
	if f.file != nil {
		f.file.Close()
	}
	return errnoSuccess
}

// fdSeek is lseek(2)
func (h *hostWasi) fdSeek(_ context.Context, m api.Module, fd uint32, offset int64, whence, newOffsetPtr uint32) uint32 {
	file, e := h.stream(fd)
	if e != errnoSuccess {
		return e
	}
	at, err := file.Seek(offset, int(whence))
	if err != nil {
		return errno(err)
	}
	m.Memory().WriteUint64Le(newOffsetPtr, uint64(at))
	return errnoSuccess
}

func (h *hostWasi) environSizesGet(_ context.Context, m api.Module, countPtr, sizePtr uint32) uint32 {
	return sizes(m, h.environ(), countPtr, sizePtr)
}

func (h *hostWasi) environGet(_ context.Context, m api.Module, ptrs, buf uint32) uint32 {
	return strings(m, h.environ(), ptrs, buf)
}

func (h *hostWasi) clockTimeGet(_ context.Context, m api.Module, _ uint32, _ uint64, ptr uint32) uint32 {
	m.Memory().WriteUint64Le(ptr, uint64(clock.Now().UnixNano()))
	return errnoSuccess
}

// randomGet is getrandom(2)
func (h *hostWasi) randomGet(_ context.Context, m api.Module, ptr, length uint32) uint32 {
	buf, ok := m.Memory().Read(ptr, length)
	if !ok {
		return errnoInval
	}
	if _, err := rand.Read(buf); err != nil {
		return errnoIO
	}
	return errnoSuccess
}

func (h *hostWasi) argsSizesGet(_ context.Context, m api.Module, countPtr, sizePtr uint32) uint32 {
	return sizes(m, h.cfg.Args, countPtr, sizePtr)
}

func (h *hostWasi) fdFdstatGet(_ context.Context, m api.Module, fd, ptr uint32) uint32 {
	var filetype byte
	f, open := h.files[fd]
	switch {
	case !open:
		return errnoBadf
	case fd <= 2:
		filetype = filetypeCharDevice
	case f.file == nil:
		filetype = filetypeDirectory
	case f.file.Store() == nil:
		filetype = filetypeCharDevice // a device, such as /dev/null
	default:
		filetype = filetypeRegular
	}
	stat := make([]byte, 24)
	stat[0] = filetype
	binary.LittleEndian.PutUint64(stat[8:], ^uint64(0))
	binary.LittleEndian.PutUint64(stat[16:], ^uint64(0))
	m.Memory().Write(ptr, stat)
	return errnoSuccess
}

func (h *hostWasi) fdPrestatGet(_ context.Context, m api.Module, fd, ptr uint32) uint32 {
	name, ok := preopenName(fd)
	if !ok {
		return errnoBadf // the end of the list of preopens
	}
	m.Memory().WriteByte(ptr, 0)                       // tag: a directory
	m.Memory().WriteUint32Le(ptr+4, uint32(len(name))) // length of its name
	return errnoSuccess
}

func (h *hostWasi) fdPrestatDirName(_ context.Context, m api.Module, fd, ptr, length uint32) uint32 {
	name, ok := preopenName(fd)
	if !ok {
		return errnoBadf
	}
	m.Memory().WriteString(ptr, name[:min(len(name), int(length))])
	return errnoSuccess
}

func (h *hostWasi) resolve(m api.Module, dirfd, ptr, length uint32) (string, uint32) {
	dir, ok := h.files[dirfd]
	if !ok {
		return "", errnoBadf
	}
	if dir.file != nil {
		return "", errnoNotDir
	}
	raw, ok := m.Memory().Read(ptr, length)
	if !ok {
		return "", errnoInval
	}
	return path.Join(dir.path, string(raw)), errnoSuccess
}

func (h *hostWasi) newFd(m api.Module, f *openFile, fdPtr uint32) uint32 {
	fd := h.next
	h.next++
	h.files[fd] = f
	m.Memory().WriteUint32Le(fdPtr, fd)
	return errnoSuccess
}

// pathOpen is open(2)
func (h *hostWasi) pathOpen(_ context.Context, m api.Module, dirfd, _, pathPtr, pathLen, oflags uint32, rights, _ uint64, fdflags, fdPtr uint32) uint32 {
	p, e := h.resolve(m, dirfd, pathPtr, pathLen)
	if e != errnoSuccess {
		return e
	}
	write := rights&rightFdWrite != 0

	info, err := h.cfg.FS.Stat(p)
	exists := err == nil
	switch {
	case err != nil && !errors.Is(err, filesystem.ErrNotExist):
		return errno(err)
	case exists && oflags&oflagCreat != 0 && oflags&oflagExcl != 0:
		return errnoExist
	case exists && info.IsDir():
		if write {
			return errnoIsDir
		}
		return h.newFd(m, &openFile{path: p}, fdPtr)
	case oflags&oflagDirectory != 0:
		if exists {
			return errnoNotDir
		}
		return errnoNoent
	}

	var flags stream.OpenFlags
	switch read := rights&rightFdRead != 0; {
	case read && write:
		flags = stream.O_RDWR
	case write:
		flags = stream.O_WRONLY
	default:
		flags = stream.O_RDONLY
	}
	if oflags&oflagCreat != 0 {
		flags |= stream.O_CREATE
	}
	if oflags&oflagTrunc != 0 {
		flags |= stream.O_TRUNC
	}
	if fdflags&fdflagAppend != 0 {
		flags |= stream.O_APPEND
	}

	file, err := h.cfg.FS.Open(p, flags)
	if err != nil {
		return errno(err)
	}
	return h.newFd(m, &openFile{path: p, file: file}, fdPtr)
}

// fdFdstatSetFlags is fcntl(F_SETFL). Go sets O_NONBLOCK on stdio at start;
func (h *hostWasi) fdFdstatSetFlags(_ context.Context, _ api.Module, fd, flags uint32) uint32 {
	if _, open := h.files[fd]; fd > 2 && !open {
		return errnoBadf
	}
	if flags&fdflagAppend != 0 {
		return errnoNotSup
	}
	return errnoSuccess
}

func (h *hostWasi) fdFilestatGet(_ context.Context, m api.Module, fd, ptr uint32) uint32 {
	if fd <= 2 {
		// stdin, stdout and stderr have no path to stat: say they are a
		// terminal-like device, which is all a guest asks them for.
		return writeFilestat(m, ptr, filesystem.Info{Mode: filesystem.ModeCharDevice})
	}
	f, ok := h.files[fd]
	if !ok {
		return errnoBadf
	}
	info, err := h.cfg.FS.Stat(f.path)
	if err != nil {
		return errno(err)
	}
	return writeFilestat(m, ptr, info)
}

// pathFilestatGet is stat(2)
func (h *hostWasi) pathFilestatGet(_ context.Context, m api.Module, dirfd, _, pathPtr, pathLen, statPtr uint32) uint32 {
	p, e := h.resolve(m, dirfd, pathPtr, pathLen)
	if e != errnoSuccess {
		return e
	}
	info, err := h.cfg.FS.Stat(p)
	if err != nil {
		return errno(err)
	}
	return writeFilestat(m, statPtr, info)
}

// pathCreateDirectory is mkdir(2).
func (h *hostWasi) pathCreateDirectory(_ context.Context, m api.Module, dirfd, pathPtr, pathLen uint32) uint32 {
	p, e := h.resolve(m, dirfd, pathPtr, pathLen)
	if e != errnoSuccess {
		return e
	}
	return errno(h.cfg.FS.Mkdir(p))
}

// pathLink is link(2): a second name for the same inode.
func (h *hostWasi) pathLink(_ context.Context, m api.Module, oldDirfd, _, oldPathPtr, oldPathLen, newDirfd, newPathPtr, newPathLen uint32) uint32 {
	from, e := h.resolve(m, oldDirfd, oldPathPtr, oldPathLen)
	if e != errnoSuccess {
		return e
	}
	to, e := h.resolve(m, newDirfd, newPathPtr, newPathLen)
	if e != errnoSuccess {
		return e
	}
	// TODO
	// VFS.Link is ln's: given a directory, it links into it. link(2) never
	// does, so a name that is already taken, directory or not, is EEXIST.
	if _, err := h.cfg.FS.Stat(to); err == nil {
		return errnoExist
	}
	return errno(h.cfg.FS.Link(from, to))
}

// pathReadlink is readlink(2)
func (h *hostWasi) pathReadlink(_ context.Context, m api.Module, dirfd, pathPtr, pathLen, _, _, _ uint32) uint32 {
	p, e := h.resolve(m, dirfd, pathPtr, pathLen)
	if e != errnoSuccess {
		return e
	}
	if _, err := h.cfg.FS.Stat(p); err != nil {
		return errno(err)
	}
	return errnoInval
}

// pathRemoveDirectory is rmdir(2): only a directory, and only an empty one.
func (h *hostWasi) pathRemoveDirectory(_ context.Context, m api.Module, dirfd, pathPtr, pathLen uint32) uint32 {
	p, e := h.resolve(m, dirfd, pathPtr, pathLen)
	if e != errnoSuccess {
		return e
	}
	info, err := h.cfg.FS.Stat(p)
	if err != nil {
		return errno(err)
	}
	if !info.IsDir() {
		return errnoNotDir
	}
	return errno(h.cfg.FS.Remove(p, false))
}

// pathRename is rename(2)
func (h *hostWasi) pathRename(_ context.Context, m api.Module, oldDirfd, oldPathPtr, oldPathLen, newDirfd, newPathPtr, newPathLen uint32) uint32 {
	from, e := h.resolve(m, oldDirfd, oldPathPtr, oldPathLen)
	if e != errnoSuccess {
		return e
	}
	to, e := h.resolve(m, newDirfd, newPathPtr, newPathLen)
	if e != errnoSuccess {
		return e
	}
	source, err := h.cfg.FS.Stat(from)
	if err != nil {
		return errno(err)
	}
	if from == to {
		return errnoSuccess
	}
	if target, err := h.cfg.FS.Stat(to); err == nil {
		switch {
		case target.IsDir() && !source.IsDir():
			return errnoIsDir
		case !target.IsDir() && source.IsDir():
			return errnoNotDir
		}
		if err := h.cfg.FS.Remove(to, false); err != nil {
			return errno(err)
		}
	}
	return errno(h.cfg.FS.Move(from, to))
}

// pathSymlink is symlink(2)
func (h *hostWasi) pathSymlink(_ context.Context, _ api.Module, _, _, _, _, _ uint32) uint32 {
	return errnoNotSup // TODO
}

// pathUnlinkFile is unlink(2)
func (h *hostWasi) pathUnlinkFile(_ context.Context, m api.Module, dirfd, pathPtr, pathLen uint32) uint32 {
	p, e := h.resolve(m, dirfd, pathPtr, pathLen)
	if e != errnoSuccess {
		return e
	}
	info, err := h.cfg.FS.Stat(p)
	if err != nil {
		return errno(err)
	}
	if info.IsDir() {
		return errnoIsDir
	}
	return errno(h.cfg.FS.Remove(p, false))
}
