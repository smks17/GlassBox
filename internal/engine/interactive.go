package engine

import (
	"path"
	"strings"
	"unicode"
)

// Prompt renders PS1 for an interactive shell.
func (app *Machine) Prompt() string {
	app.op.Lock()
	defer app.op.Unlock()
	session := app.session
	format, ok := session.shell.Vars()["PS1"]
	if !ok {
		format = `\u\$ `
	}
	sigil := "$"
	if session.fs.Identity().IsRoot() {
		sigil = "#"
	}
	prompt := strings.NewReplacer(
		`\u`, session.User(),
		`\w`, session.Cwd(),
		`\W`, path.Base(session.Cwd()),
		`\$`, sigil,
		`\t`, session.at(),
		`\\`, `\`,
	).Replace(format)
	// A prompt can contain virtual filenames or values set by shell code.
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, prompt)
}
