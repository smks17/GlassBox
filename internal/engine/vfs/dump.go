package vfs

import (
	"fmt"
	"strings"
	"time"

	"parrot/internal/engine/filesystem"
)

type DumpNode struct {
	Name     string      `json:"name"`
	Content  []byte      `json:"content,omitempty"`
	Children []*DumpNode `json:"children,omitempty"`

	Mode  *filesystem.FileMode `json:"mode,omitempty"`
	Owner string               `json:"owner,omitempty"`
	Group string               `json:"group,omitempty"`

	// Mtime is Unix seconds.
	Mtime int64 `json:"mtime,omitempty"`

	Ino uint64 `json:"ino,omitempty"`
}

const (
	maxRestoreNodes = 100000
	maxRestoreDepth = 256
)

func (node *Inode) Dump(name string) *DumpNode {
	return node.dump(name, map[Ino]bool{})
}

func (node *Inode) dump(name string, written map[Ino]bool) *DumpNode {
	d := &DumpNode{Name: name, Ino: uint64(node.ino)}
	if written[node.ino] {
		return d
	}
	written[node.ino] = true

	owner, mode := node.meta()
	d.Mode, d.Owner, d.Group = &mode, owner.User, owner.Group
	d.Content = node.Bytes()
	// A zero Time means "unknown"
	if mtime := node.ModTime(); !mtime.IsZero() {
		d.Mtime = mtime.Unix()
	}
	for _, entry := range node.Entries() {
		if entry.Inode.Device() != nil {
			continue // TODO
		}
		// sysfs is the running machine's, made again on restore, never saved.
		if name == "" && entry.Name == sysDir {
			continue
		}
		d.Children = append(d.Children, entry.Inode.dump(entry.Name, written))
	}
	return d
}

func (d *DumpNode) ToInode() (*Inode, error) {
	remaining := maxRestoreNodes
	return d.toInode(map[uint64]*Inode{}, true, 0, &remaining)
}

func (d *DumpNode) toInode(byIno map[uint64]*Inode, isRoot bool, depth int, remaining *int) (*Inode, error) {
	if d == nil || depth > maxRestoreDepth || *remaining == 0 {
		return nil, fmt.Errorf("snapshot tree is missing or exceeds restore limits: %w", filesystem.ErrInvalid)
	}
	*remaining -= 1
	if d.Ino != 0 {
		if linked, ok := byIno[d.Ino]; ok {
			// Only regular files may have additional names. An alias carries
			// no second definition of the inode's metadata or contents.
			if linked.IsDir() || d.Mode != nil || d.Owner != "" || d.Group != "" || d.Mtime != 0 || len(d.Content) != 0 || len(d.Children) != 0 {
				return nil, fmt.Errorf("snapshot inode %d has an invalid alias: %w", d.Ino, filesystem.ErrInvalid)
			}
			return linked, nil
		}
	}

	owner := Ownership{User: d.Owner, Group: d.Group}

	mode, known := filesystem.DefaultFileMode, false
	if d.Mode != nil {
		mode, known = *d.Mode, true
		if mode & ^(filesystem.ModeDirectory|0777) != 0 || (isRoot && !mode.IsDirectory()) {
			return nil, fmt.Errorf("snapshot entry %q has an invalid mode: %w", d.Name, filesystem.ErrInvalid)
		}
	}
	isDir := isRoot || mode.IsDirectory() || (!known && len(d.Children) > 0)
	if !isDir && len(d.Children) > 0 {
		return nil, fmt.Errorf("snapshot file %q has children: %w", d.Name, filesystem.ErrInvalid)
	}

	var n *Inode
	switch {
	case isRoot:
		n = newRoot(owner)
	case isDir:
		n = NewDir(owner)
	default:
		n = NewFile(d.Content, owner)
	}
	if known {
		n.setOwner(owner, mode)
	}
	if d.Ino != 0 {
		byIno[d.Ino] = n
	}

	for _, child := range d.Children {
		if child == nil || child.Name == "" || child.Name == currDir || child.Name == preDir || strings.ContainsAny(child.Name, "/\x00") {
			return nil, fmt.Errorf("snapshot contains an invalid directory entry: %w", filesystem.ErrInvalid)
		}
		inode, err := child.toInode(byIno, false, depth+1, remaining)
		if err != nil {
			return nil, err
		}
		if err := n.Link(child.Name, inode); err != nil {
			return nil, fmt.Errorf("restore snapshot entry %q: %w", child.Name, err)
		}
	}

	if d.Mtime != 0 {
		n.lock(func() { n.mtime = time.Unix(d.Mtime, 0) })
	}
	return n, nil
}
