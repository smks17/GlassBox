package engine

import (
	"fmt"
	"time"

	"glass/internal/engine/user"
)

const sudoAuthenticationLifetime = 5 * time.Minute

func (s *Session) authorizeSudo(caller user.Identity, target string, readPassword func() (string, error)) (user.Identity, error) {
	tree, ok := s.tree()
	if !ok {
		return user.Identity{}, ErrRealFilesystem
	}
	db := tree.UsersDB()
	current, err := db.Lookup(caller.Name)
	if err != nil || current.UID != caller.UID {
		return user.Identity{}, fmt.Errorf("unknown caller")
	}
	identity, err := db.Lookup(target)
	if err != nil {
		return user.Identity{}, err
	}
	if !current.IsRoot() {
		if !current.InGroup(user.SudoGroup) {
			return user.Identity{}, fmt.Errorf("%s is not in the sudo group", current.Name)
		}
		s.sudoMutex.Lock()
		cached := time.Now().Before(s.sudoExpires[current.Name])
		generation := s.sudoGeneration
		s.sudoMutex.Unlock()
		if cached {
			return identity, nil
		}
		password, err := readPassword()
		if err != nil {
			return user.Identity{}, err
		}
		if err := db.Authenticate(current.Name, password); err != nil {
			return user.Identity{}, fmt.Errorf("authentication failed")
		}
		// Account policy may change while the terminal waits for a password.
		current, err = db.Lookup(caller.Name)
		if err != nil || current.UID != caller.UID || !current.InGroup(user.SudoGroup) {
			return user.Identity{}, fmt.Errorf("sudo authorization changed during authentication")
		}
		identity, err = db.Lookup(target)
		if err != nil {
			return user.Identity{}, err
		}
		s.sudoMutex.Lock()
		defer s.sudoMutex.Unlock()
		if generation != s.sudoGeneration {
			return user.Identity{}, fmt.Errorf("session changed during authentication")
		}
		if s.sudoExpires == nil {
			s.sudoExpires = make(map[string]time.Time)
		}
		s.sudoExpires[current.Name] = time.Now().Add(sudoAuthenticationLifetime)
	}
	return identity, nil
}
