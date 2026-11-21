package engine

import (
	"path"

	"glass/internal/engine/dev"
	"glass/internal/engine/network"
	"glass/internal/engine/vfs"
)

const sysClassNet = "/sys/class/net"

func (app *Machine) seedSys() {
	tree, inMemory := app.session.tree()
	if !inMemory {
		return
	}
	nm := app.network
	add := func(name string) { addSysInterface(tree, nm, name) }
	nm.OnNewInterface(add)
	for _, name := range nm.Interfaces() {
		add(name)
	}
}

func addSysInterface(tree *vfs.VFS, nm *network.NetworkManager, name string) {
	dir := path.Join(sysClassNet, name)
	if tree.MakeDirAll(dir) != nil {
		return
	}
	operstate := dev.Generated{Content: func() []byte {
		if up, _ := nm.LinkUp(name); up {
			return []byte("up\n")
		}
		return []byte("down\n")
	}}
	_ = tree.MakeDevice(path.Join(dir, "operstate"), operstate, 0444)
}
