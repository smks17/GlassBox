package commands

const (
	EnvUser = "USER"
	EnvHome = "HOME"
	EnvPath = "PATH"
)

const DefaultPath = "/usr/bin:/bin"

func (ctx *Context) Home() string {
	if home := ctx.Env[EnvHome]; home != "" {
		return home
	}
	return ctx.User.Home
}
