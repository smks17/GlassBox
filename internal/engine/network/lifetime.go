package network

import (
	"io"
	"net/netip"
	"sync"
)

// Resource ownership also covers interfaces such as the optional host network,
// which do not themselves implement Close.
func (nm *NetworkManager) own(resource io.Closer) error {
	nm.mu.Lock()
	if nm.down {
		nm.mu.Unlock()
		resource.Close()
		return ErrNetDown
	}
	nm.resources[resource] = struct{}{}
	nm.mu.Unlock()
	return nil
}

func (nm *NetworkManager) forget(resource io.Closer) {
	nm.mu.Lock()
	delete(nm.resources, resource)
	nm.mu.Unlock()
}

type ownedConn struct {
	Conn
	manager *NetworkManager
	once    sync.Once
	err     error
}

func (c *ownedConn) Close() error {
	c.once.Do(func() {
		c.err = c.Conn.Close()
		c.manager.forget(c)
	})
	return c.err
}
func (c *ownedConn) MayBlock() bool { return true }

func (nm *NetworkManager) ownConn(conn Conn) (Conn, error) {
	c := &ownedConn{Conn: conn, manager: nm}
	if err := nm.own(c); err != nil {
		return nil, err
	}
	return c, nil
}

type ownedListener struct {
	Listener
	manager *NetworkManager
	once    sync.Once
	err     error
}

func (l *ownedListener) Close() error {
	l.once.Do(func() {
		l.err = l.Listener.Close()
		l.manager.forget(l)
	})
	return l.err
}
func (l *ownedListener) Accept() (Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return l.manager.ownConn(c)
}
func (nm *NetworkManager) listenOn(n Network, ip netip.Addr, port uint16, backlog int) (Listener, error) {
	listener, err := n.Listen(ip, port, backlog)
	if err != nil {
		return nil, err
	}
	l := &ownedListener{Listener: listener, manager: nm}
	if err := nm.own(l); err != nil {
		return nil, err
	}
	return l, nil
}
