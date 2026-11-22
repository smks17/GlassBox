package engine

import (
	"encoding/json"
	"errors"
	"time"

	"glass/internal/engine/clock"
	"glass/internal/engine/commands"
	"glass/internal/engine/vfs"
)

// TODO: Implement this
var ErrRealFilesystem = errors.New("this session runs on the real filesystem")

// MaxSnapshotBytes limits the size of the encoded snapshot JSON.
const MaxSnapshotBytes = 16 << 20

// The first release saved snapshots without recording its version.
var legacySnapshotVersion = Version{Major: 0, Minor: 1, Patch: 0}

// Raise this when a release can no longer read earlier snapshots.
var oldestCompatibleSnapshotVersion = Version{Major: 0, Minor: 1, Patch: 0}

var (
	ErrSnapshotTooLarge = errors.New("snapshot exceeds 16 MiB")
	ErrSnapshotVersion  = errors.New("incompatible snapshot app version")
)

type snapshot struct {
	AppVersion *Version          `json:"version"`
	Root       *vfs.DumpNode     `json:"root"`
	Cwd        string            `json:"cwd"`
	Env        map[string]string `json:"env"`
	History    []historyRecord   `json:"history"`
	User       string            `json:"user,omitempty"`
	// Clock is how far `date -s` moved the shell's clock, in nanoseconds, and
	// Zone/ZoneOffset is the captured default zone — a shared session
	// shows the same times to whoever opens it. TZ rides along in Env.
	Clock      int64  `json:"clock_offset,omitempty"`
	Zone       string `json:"zone,omitempty"`
	ZoneOffset int    `json:"zone_offset,omitempty"`
}

type historyRecord struct {
	Line string `json:"line"`
	At   int64  `json:"at,omitempty"`
}

func (h *historyRecord) UnmarshalJSON(data []byte) error {
	var line string
	if err := json.Unmarshal(data, &line); err == nil {
		h.Line, h.At = line, 0
		return nil
	}
	type record historyRecord
	var r record
	if err := json.Unmarshal(data, &r); err != nil {
		return err
	}
	*h = historyRecord(r)
	return nil
}

func (s *Session) Snapshot() ([]byte, error) {
	tree, inMemory := s.tree()
	if !inMemory {
		return nil, ErrRealFilesystem
	}

	history := make([]historyRecord, len(s.shell.History))
	for i, entry := range s.shell.History {
		history[i] = historyRecord{Line: entry.Line}
		if !entry.At.IsZero() {
			history[i].At = entry.At.Unix()
		}
	}
	appVersion := AppVersion
	snap := snapshot{
		AppVersion: &appVersion,
		Root:       tree.RootInode().Dump(""),
		Cwd:        tree.Cwd(),
		Env:        s.shell.Vars(),
		History:    history,
		User:       s.User(),
		Clock:      int64(s.clock.Offset()),
	}
	snap.Zone, snap.ZoneOffset = s.clock.Local()
	data, err := json.Marshal(snap)
	if err != nil {
		return nil, err
	}
	if len(data) > MaxSnapshotBytes {
		return nil, ErrSnapshotTooLarge
	}
	return data, nil
}

func snapshotCompatible(saved, oldest, machine Version) bool {
	return oldest.Compare(saved) <= 0 && saved.Compare(machine) <= 0
}

func LoadSession(data []byte) (*Session, error) {
	if len(data) > MaxSnapshotBytes {
		return nil, ErrSnapshotTooLarge
	}
	legacyVersion := legacySnapshotVersion
	snap := snapshot{AppVersion: &legacyVersion}
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, err
	}
	if snap.AppVersion == nil || !snapshotCompatible(*snap.AppVersion, oldestCompatibleSnapshotVersion, AppVersion) {
		return nil, ErrSnapshotVersion
	}
	root, err := snap.Root.ToInode()
	if err != nil {
		return nil, err
	}
	c := clock.New()
	c.SetOffset(time.Duration(snap.Clock))
	if snap.Zone != "" {
		c.SetLocal(snap.Zone, snap.ZoneOffset)
	}
	session := newSession(vfs.FromRootWithClock(root, snap.Cwd, c), c)

	if snap.User != "" {
		session.SetUser(snap.User)
	}

	if snap.Env != nil {
		session.shell.SetVars(snap.Env)
		session.SetUser(session.User())
	}
	history := make([]commands.HistoryEntry, len(snap.History))
	for i, record := range snap.History {
		history[i] = commands.HistoryEntry{Line: record.Line}
		if record.At != 0 {
			history[i].At = time.Unix(record.At, 0)
		}
	}
	session.shell.History = append([]commands.HistoryEntry(nil), history...)
	return session, nil
}
