package commands

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"glass/internal/engine/filesystem"
	"glass/internal/engine/signal"
	"glass/internal/engine/user"
	"glass/internal/engine/vfs"
)

func identityCommandContext(t *testing.T) (*Context, *vfs.VFS, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	fs := vfs.New()
	id, err := fs.UsersDB().Lookup("mahdi")
	if err != nil {
		t.Fatal(err)
	}
	fs.SetIdentity(id)
	out, diagnostic := new(bytes.Buffer), new(bytes.Buffer)
	return &Context{VFS: fs, Stdout: out, Stderr: diagnostic}, fs, out, diagnostic
}

func identityCommand(t *testing.T, name string) Command {
	t.Helper()
	c, ok := Lookup(name)
	if !ok {
		t.Fatalf("%s is not registered", name)
	}
	return c
}

func TestIDSimulatedIdentity(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{nil, "uid=1000(mahdi) gid=1000(users) groups=1000(users),100(dev)\n"},
		{[]string{"-u"}, "1000\n"}, {[]string{"-unr"}, "mahdi\n"},
		{[]string{"-gn"}, "users\n"}, {[]string{"-g"}, "1000\n"},
		{[]string{"-G"}, "1000 100\n"}, {[]string{"-Gn"}, "users dev\n"},
	} {
		ctx, _, out, diagnostic := identityCommandContext(t)
		ctx.User = user.Identity{Name: "wrong", UID: 99}
		if code := identityCommand(t, "id").Run(ctx, test.args); code != 0 || out.String() != test.want || diagnostic.Len() != 0 {
			t.Fatalf("%v: code=%d out=%q err=%q", test.args, code, out, diagnostic)
		}
	}
}

func TestIDRootAndUnnamedIdentity(t *testing.T) {
	for _, test := range []struct {
		id   user.Identity
		args []string
		want string
	}{
		{user.Identity{Name: "root", Groups: []user.Group{{Name: "root"}}}, nil, "uid=0(root) gid=0(root) groups=0(root)\n"},
		{user.Identity{UID: 42, GID: 7}, nil, "uid=42 gid=7 groups=7\n"},
		{user.Identity{UID: 42, GID: 7}, []string{"-un"}, "42\n"},
		{user.Identity{UID: 42, GID: 7}, []string{"-Gn"}, "7\n"},
		{user.Identity{UID: 1000, GID: 9999, Groups: []user.Group{{Name: "dev", GID: 100}}}, nil, "uid=1000 gid=9999 groups=9999,100(dev)\n"},
		{user.Identity{UID: 1000, GID: 9999, Groups: []user.Group{{Name: "dev", GID: 100}}}, []string{"-gn"}, "9999\n"},
		{user.Identity{UID: 1000, GID: 9999, Groups: []user.Group{{Name: "dev", GID: 100}}}, []string{"-G"}, "9999 100\n"},
		{user.Identity{UID: 42, GID: 7, Groups: []user.Group{{Name: "extra", GID: 8}, {Name: "primary", GID: 7}, {Name: "extra", GID: 8}}}, []string{"-Gn"}, "primary extra\n"},
	} {
		ctx, fs, out, _ := identityCommandContext(t)
		fs.SetIdentity(test.id)
		if code := identityCommand(t, "id").Run(ctx, test.args); code != 0 || out.String() != test.want {
			t.Fatalf("code=%d out=%q want=%q", code, out, test.want)
		}
	}
}

func TestIDInvalidArguments(t *testing.T) {
	for _, args := range [][]string{{"-ug"}, {"-uG"}, {"-gG"}, {"-n"}, {"-r"}, {"-z"}, {"mahdi"}, {"--", "root"}, {"--help"}} {
		ctx, _, out, diagnostic := identityCommandContext(t)
		if code := identityCommand(t, "id").Run(ctx, args); code == 0 || out.Len() != 0 || !strings.HasPrefix(diagnostic.String(), "id:") {
			t.Fatalf("%v code=%d out=%q err=%q", args, code, out, diagnostic)
		}
	}
}

type identityFailureWriter struct {
	err   error
	short bool
}

func (w identityFailureWriter) Write(p []byte) (int, error) {
	if w.short {
		return len(p) - 1, nil
	}
	return 0, w.err
}

func TestIdentityCommandsOutputAndCancellation(t *testing.T) {
	for _, name := range []string{"id", "stat", "uname"} {
		args := []string(nil)
		if name == "stat" {
			args = []string{"/README.md"}
		}
		for _, test := range []struct {
			writer io.Writer
			want   int
		}{
			{identityFailureWriter{err: filesystem.ErrBrokenPipe}, 141},
			{identityFailureWriter{err: signal.ErrInterrupted}, 130},
			{identityFailureWriter{err: errors.New("output failed")}, 1},
			{identityFailureWriter{short: true}, 1},
		} {
			ctx, _, _, _ := identityCommandContext(t)
			ctx.Stdout = test.writer
			if code := identityCommand(t, name).Run(ctx, args); code != test.want {
				t.Fatalf("%s output code=%d want=%d", name, code, test.want)
			}
		}
		ctx, _, out, _ := identityCommandContext(t)
		cancelled, cancel := context.WithCancel(context.Background())
		cancel()
		ctx.Ctx = cancelled
		if code := identityCommand(t, name).Run(ctx, args); code != 130 || out.Len() != 0 {
			t.Fatalf("%s cancelled code=%d out=%q", name, code, out)
		}
	}
}
