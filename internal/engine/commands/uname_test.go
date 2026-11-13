package commands

import "testing"

func TestUnameVirtualIdentity(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{nil, "Parrot\n"}, {[]string{"-a"}, "Parrot parrot 0.1.0 virtual virtual Parrot\n"},
		{[]string{"-o"}, "Parrot\n"}, {[]string{"-n"}, "parrot\n"},
		{[]string{"-r"}, "0.1.0\n"}, {[]string{"-v"}, "virtual\n"}, {[]string{"-m"}, "virtual\n"},
		{[]string{"-onmsrvs"}, "Parrot parrot 0.1.0 virtual virtual Parrot\n"},
		{[]string{"-mn", "-s", "--"}, "Parrot parrot virtual\n"}, {[]string{"--"}, "Parrot\n"},
	} {
		ctx, _, out, diagnostic := identityCommandContext(t)
		if code := identityCommand(t, "uname").Run(ctx, test.args); code != 0 || out.String() != test.want || diagnostic.Len() != 0 {
			t.Fatalf("%v code=%d out=%q err=%q", test.args, code, out, diagnostic)
		}
	}
}

func TestUnameRejectsArguments(t *testing.T) {
	for _, args := range [][]string{{"-z"}, {"--help"}, {"host"}, {"--", "-a"}, {"-"}} {
		ctx, _, out, diagnostic := identityCommandContext(t)
		if code := identityCommand(t, "uname").Run(ctx, args); code == 0 || out.Len() != 0 || diagnostic.Len() == 0 {
			t.Fatalf("%v code=%d out=%q err=%q", args, code, out, diagnostic)
		}
	}
}
