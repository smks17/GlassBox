package vfs

import "strings"

var binDirs = []string{"bin", "usr", "usr/bin", "usr/lib"}

func seedBin(root *Inode) {
	for _, p := range binDirs {
		dir := root
		for _, name := range strings.Split(p, "/") {
			next, ok := dir.Lookup(name)
			if !ok {
				next = NewDir(rootOwner())
				if dir.Link(name, next) != nil {
					return
				}
			}
			if !next.IsDir() {
				break // a file sits where the directory goes
			}
			dir = next
		}
	}
}
