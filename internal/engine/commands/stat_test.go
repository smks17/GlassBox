package commands

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
)

type statCancellingWriter struct {
	buffer bytes.Buffer
	cancel context.CancelFunc
}

func (w *statCancellingWriter) Write(p []byte) (int, error) {
	n, err := w.buffer.Write(p)
	w.cancel()
	return n, err
}

func (w *statCancellingWriter) String() string { return w.buffer.String() }

func TestStatCancellationStopsBetweenOperands(t *testing.T) {
	ctx, _, _, diagnostic := identityCommandContext(t)
	cancelled, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx.Ctx = cancelled
	out := &statCancellingWriter{cancel: cancel}
	ctx.Stdout = out
	if code := identityCommand(t, "stat").Run(ctx, []string{"/README.md", "missing"}); code != 130 || diagnostic.Len() != 0 || !strings.Contains(out.String(), "File: /README.md") {
		t.Fatalf("code=%d out=%q err=%q", code, out, diagnostic)
	}
}

func TestStatSimulatedMetadata(t *testing.T) {
	ctx, fs, out, diagnostic := identityCommandContext(t)
	if err := fs.Create("note"); err != nil {
		t.Fatal(err)
	}
	if err := fs.Write("note", []byte("hello"), false); err != nil {
		t.Fatal(err)
	}
	if err := fs.Chmod("note", 0640); err != nil {
		t.Fatal(err)
	}
	if err := fs.Link("note", "alias"); err != nil {
		t.Fatal(err)
	}
	info, err := fs.Stat("note")
	if err != nil {
		t.Fatal(err)
	}
	if code := identityCommand(t, "stat").Run(ctx, []string{"note"}); code != 0 || diagnostic.Len() != 0 {
		t.Fatalf("code=%d err=%q", code, diagnostic)
	}
	for _, want := range []string{"File: note", "Size: 5", "regular file", fmt.Sprintf("Inode: %d", info.Ino), "Links: 2", "0640/-rw-r-----", "User: mahdi", "Group: users", info.ModTime.Format("2006-01-02 15:04:05 -0700")} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("out=%q missing %q", out, want)
		}
	}
	if strings.Contains(out.String(), "uid=0") || strings.Contains(out.String(), "(0/") {
		t.Fatalf("invented numeric ownership: %q", out)
	}
}

func TestStatTypesAndOptionTerminator(t *testing.T) {
	ctx, fs, out, diagnostic := identityCommandContext(t)
	if err := fs.Create("-file"); err != nil {
		t.Fatal(err)
	}
	if code := identityCommand(t, "stat").Run(ctx, []string{"--", "-file", ".", "/dev/null"}); code != 0 || diagnostic.Len() != 0 {
		t.Fatalf("code=%d err=%q", code, diagnostic)
	}
	for _, want := range []string{"File: -file", "directory", "character special file"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q: %q", want, out)
		}
	}
}

func TestStatContinuesAfterLookupAndPermissionErrors(t *testing.T) {
	ctx, fs, out, diagnostic := identityCommandContext(t)
	if err := fs.Mkdir("private"); err != nil {
		t.Fatal(err)
	}
	if err := fs.Create("private/hidden"); err != nil {
		t.Fatal(err)
	}
	if err := fs.Chmod("private", 0600); err != nil {
		t.Fatal(err)
	}
	if code := identityCommand(t, "stat").Run(ctx, []string{"absent", "private/hidden", "/README.md"}); code != 1 {
		t.Fatalf("code=%d", code)
	}
	if !strings.Contains(diagnostic.String(), "absent") || !strings.Contains(diagnostic.String(), "permission denied") || !strings.Contains(out.String(), "File: /README.md") {
		t.Fatalf("out=%q err=%q", out, diagnostic)
	}
}

func TestStatRejectsMissingOperandsAndOptions(t *testing.T) {
	for _, args := range [][]string{nil, {"-x"}, {"--"}} {
		ctx, _, out, diagnostic := identityCommandContext(t)
		if code := identityCommand(t, "stat").Run(ctx, args); code == 0 || out.Len() != 0 || diagnostic.Len() == 0 {
			t.Fatalf("args=%v code=%d out=%q err=%q", args, code, out, diagnostic)
		}
	}
}
