package systemd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"glass/internal/engine/filesystem"
	"glass/internal/engine/proc"
	"glass/internal/engine/user"
)

const (
	journalDir       = "/var/log/journal"
	journalPath      = journalDir + "/system.jsonl"
	journalChunkSize = 4096
	journalFileLimit = 4 * 1024 * 1024
)

type journalEntry struct {
	Unit   string
	PID    proc.PID
	Stderr bool
	Text   []byte
}

type journal struct {
	mu      sync.Mutex
	fs      filesystem.FS
	initErr error
}

func (j *journal) write(entry journalEntry, data []byte) error {
	if j.initErr != nil {
		return j.initErr
	}

	// Retain the tail of oversized writes so one write cannot exhaust memory.
	if len(data) > journalChunkSize {
		data = data[len(data)-journalChunkSize:]
	}
	entry.Text = data
	record, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	record = append(record, '\n')
	if len(record) > journalFileLimit {
		return ErrJournalTooLarge
	}

	j.mu.Lock()
	defer j.mu.Unlock()
	size, err := j.fileSize()
	if err != nil {
		return err
	}
	if size+int64(len(record)) <= journalFileLimit {
		return j.fs.Write(journalPath, record, true)
	}

	// Compact at a record boundary and leave room for future appends.
	content, err := j.fs.Read(journalPath)
	if err != nil {
		return err
	}
	cut := max(len(content)/2, len(content)+len(record)-journalFileLimit)
	if newline := bytes.IndexByte(content[cut:], '\n'); newline >= 0 {
		record = append(bytes.Clone(content[cut+newline+1:]), record...)
	}
	return j.fs.Write(journalPath, record, false)
}

func (j *journal) read(unit string, lineLimit int) (string, error) {
	if j.initErr != nil {
		return "", j.initErr
	}
	if lineLimit == 0 {
		return "", nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if _, err := j.fileSize(); err != nil {
		if errors.Is(err, filesystem.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	content, err := j.fs.Read(journalPath)
	if err != nil {
		return "", err
	}
	var output strings.Builder
	if len(content) > 0 {
		for record := range bytes.SplitSeq(bytes.TrimSuffix(content, []byte("\n")), []byte("\n")) {
			var entry journalEntry
			if err := json.Unmarshal(record, &entry); err != nil {
				return "", fmt.Errorf("decode %s: %w", journalPath, err)
			}
			if unit == "" || entry.Unit == unit {
				output.Write(entry.Text)
			}
		}
	}
	text := output.String()
	if lineLimit < 0 {
		return text, nil
	}

	// Count line breaks across writes, ignoring a newline at the very end.
	end := len(strings.TrimSuffix(text, "\n"))
	for range lineLimit {
		end = strings.LastIndexByte(text[:end], '\n')
		if end < 0 {
			return text, nil
		}
	}
	return text[end+1:], nil
}

var journalDirectories = [...]string{"/var", "/var/log", journalDir}

func (j *journal) initialize() error {
	if _, err := j.fileSize(); !errors.Is(err, filesystem.ErrNotExist) {
		return err
	}
	for _, dir := range journalDirectories {
		if err := j.fs.Mkdir(dir); err != nil {
			if errors.Is(err, filesystem.ErrExists) {
				continue
			}
			return err
		}
		if dir == journalDir {
			if err := j.fs.Chmod(dir, 0700); err != nil {
				return err
			}
		}
	}
	return j.fs.Create(journalPath)
}

func (j *journal) fileSize() (int64, error) {
	stat := j.fs.Stat
	if links, ok := j.fs.(filesystem.SymlinkFS); ok {
		stat = links.Lstat
	}
	for _, dir := range journalDirectories {
		info, err := stat(dir)
		if err != nil {
			return 0, err
		}
		if !info.IsDir() || info.Owner.Name != user.RootName || info.Mode&0022 != 0 {
			return 0, fmt.Errorf("%s: %w", dir, filesystem.ErrPermission)
		}
	}

	info, err := stat(journalPath)
	if err != nil {
		return 0, err
	}
	if info.Mode&(filesystem.ModeDirectory|filesystem.ModeCharDevice|filesystem.ModeSymlink) != 0 ||
		info.Owner.Name != user.RootName || info.Mode&0022 != 0 || info.Links != 1 {
		return 0, fmt.Errorf("%s: %w", journalPath, filesystem.ErrPermission)
	}
	if info.Size > journalFileLimit {
		return 0, ErrJournalTooLarge
	}
	return info.Size, nil
}

type journalWriter struct {
	journal *journal
	entry   journalEntry
}

func (w journalWriter) Write(data []byte) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}
	if err := w.journal.write(w.entry, data); err != nil {
		return 0, err
	}
	return len(data), nil
}
