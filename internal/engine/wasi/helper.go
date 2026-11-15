package wasi

import (
	"encoding/binary"
	"errors"
	"glass/internal/engine/filesystem"

	"github.com/tetratelabs/wazero/api"
)

const maxIovecs = 1024

func strings(m api.Module, list []string, ptrs, buf uint32) uint32 {
	mem := m.Memory()
	for i, s := range list {
		if !mem.WriteUint32Le(ptrs+uint32(i)*4, buf) || !mem.WriteString(buf, s) || !mem.WriteByte(buf+uint32(len(s)), 0) {
			return errnoInval
		}
		buf += uint32(len(s)) + 1
	}
	return errnoSuccess
}

func iovecs(m api.Module, ptr, count uint32) ([][]byte, bool) {
	if count > maxIovecs {
		return nil, false
	}
	mem := m.Memory()
	vecs := make([][]byte, 0, count)
	for i := range count {
		off := uint64(ptr) + uint64(i)*8
		if off+8 > uint64(mem.Size()) {
			return nil, false
		}
		raw, ok := mem.Read(uint32(off), 8)
		if !ok {
			return nil, false
		}
		buf, ok := mem.Read(
			binary.LittleEndian.Uint32(raw),
			binary.LittleEndian.Uint32(raw[4:]),
		)
		if !ok {
			return nil, false
		}
		vecs = append(vecs, buf)
	}
	return vecs, true
}

func sizes(m api.Module, list []string, countPtr, sizePtr uint32) uint32 {
	size := 0
	for _, s := range list {
		size += len(s) + 1
	}
	if !m.Memory().WriteUint32Le(countPtr, uint32(len(list))) || !m.Memory().WriteUint32Le(sizePtr, uint32(size)) {
		return errnoInval
	}
	return errnoSuccess
}

func preopenName(fd uint32) (string, bool) {
	if fd == preopenFd {
		return "/", true
	}
	return "", false
}

// errno turns a Glassbox error into the WASI number for it; nil is success, so
// a call can end in return errno(g.cfg.FS.Mkdir(p)).
func errno(err error) uint32 {
	switch {
	case err == nil:
		return errnoSuccess
	case errors.Is(err, filesystem.ErrNotExist):
		return errnoNoent
	case errors.Is(err, filesystem.ErrPermission):
		return errnoAccess
	case errors.Is(err, filesystem.ErrIsDir):
		return errnoIsDir
	case errors.Is(err, filesystem.ErrExists):
		return errnoExist
	case errors.Is(err, filesystem.ErrNotDir):
		return errnoNotDir
	case errors.Is(err, filesystem.ErrNotOwner):
		return errnoPerm
	case errors.Is(err, filesystem.ErrInvalid):
		return errnoInval
	case errors.Is(err, filesystem.ErrNotEmptyDir):
		return errnoNotEmpty
	case errors.Is(err, filesystem.ErrIllegalSeek):
		return errnoSpipe
	case errors.Is(err, filesystem.ErrBadFD):
		return errnoBadf
	case errors.Is(err, filesystem.ErrLinkDir):
		return errnoPerm // what link(2) says of a directory
	case errors.Is(err, filesystem.ErrRootRemove):
		return errnoBusy // what rmdir(2) says of "/"
	case errors.Is(err, filesystem.ErrNotImplemented):
		return errnoNosys
	}
	return errnoIO
}

// writeFilestat writes WASI's 64-byte filestat: device, inode, type, links,
// size, then access, modify and change times.
func writeFilestat(m api.Module, ptr uint32, info filesystem.Info) uint32 {
	stat := make([]byte, 64)
	binary.LittleEndian.PutUint64(stat[8:], info.Ino)
	stat[16] = filetype(info.Mode)
	binary.LittleEndian.PutUint64(stat[24:], uint64(info.Links))
	binary.LittleEndian.PutUint64(stat[32:], uint64(info.Size))
	for _, at := range []int{40, 48, 56} {
		binary.LittleEndian.PutUint64(stat[at:], uint64(info.ModTime.UnixNano()))
	}
	if !m.Memory().Write(ptr, stat) {
		return errnoInval
	}
	return errnoSuccess
}

func filetype(mode filesystem.FileMode) byte {
	switch {
	case mode.IsDirectory():
		return filetypeDirectory
	case mode.IsCharDevice():
		return filetypeCharDevice
	}
	return filetypeRegular
}
