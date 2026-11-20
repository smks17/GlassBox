package vfs

import (
	"strings"

	"glass/internal/engine/filesystem"
)

func (f *VFS) Symlink(target, name string) error {
	if target == "" || strings.ContainsRune(target, 0) {
		return filesystem.ErrInvalid
	}
	owner := OwnedBy(f.id)
	node := NewFile([]byte(target), owner).setOwner(owner, filesystem.ModeSymlink|0777)
	return f.create(name, node)
}

func (f *VFS) Readlink(name string) (string, error) {
	node, err := f.resolve(f.cwd, name, keepFinalLink, 0)
	if err != nil {
		return "", err
	}
	if node.Mode()&filesystem.ModeSymlink == 0 {
		return "", filesystem.ErrInvalid
	}
	return string(node.Bytes()), nil
}

func (f *VFS) Lstat(name string) (filesystem.Info, error) {
	node, err := f.resolve(f.cwd, name, keepFinalLink, 0)
	if err != nil {
		return filesystem.Info{}, err
	}
	return info(name, node), nil
}
