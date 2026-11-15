package wasi

import (
	"context"
	"errors"
	"fmt"
	"io"

	"glass/internal/engine/filesystem"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental"
	"github.com/tetratelabs/wazero/sys"
)

const preopenFd = 3

const (
	errnoSuccess  = 0
	errnoAccess   = 2
	errnoBadf     = 8
	errnoBusy     = 10
	errnoExist    = 20
	errnoIntr     = 27
	errnoInval    = 28
	errnoIO       = 29
	errnoIsDir    = 31
	errnoNoent    = 44
	errnoNosys    = 52
	errnoNotDir   = 54
	errnoNotEmpty = 55
	errnoNotSup   = 58
	errnoPerm     = 63
	errnoSpipe    = 70
)

type Cache = wazero.CompilationCache

type WasiContext struct {
	FS     filesystem.FS
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Args   []string // argv, with argv[0] the program's name
	Env    map[string]string
	Cache  Cache // nil compiles every module afresh
}

func procExit(ctx context.Context, m api.Module, stack []uint64) {
	code := uint32(stack[0])
	_ = m.CloseWithExitCode(ctx, code)
	panic(sys.NewExitError(code))
}

func Run(ctx context.Context, module []byte, cfg WasiContext) int {
	if ctx == nil {
		ctx = context.Background()
	}

	config := wazero.NewRuntimeConfig().WithCloseOnContextDone(true)
	if cfg.Cache != nil {
		config = config.WithCompilationCache(cfg.Cache)
	}
	runtime := wazero.NewRuntimeWithConfig(ctx, config)
	defer runtime.Close(ctx)

	if cfg.Env["WASI_TRACE"] == "1" {
		ctx = experimental.WithFunctionListenerFactory(ctx, tracer{cfg.Stderr})
	}

	compiled, err := runtime.CompileModule(ctx, module)
	if err != nil {
		fmt.Fprintf(cfg.Stderr, "wasi: %v\n", err)
		return 126
	}

	host := &hostWasi{
		cfg: cfg,
		files: map[uint32]*openFile{
			preopenFd: {path: "/"},
		},
		next: preopenFd + 1,
	}
	if err := host.instantiateHost(ctx, runtime, compiled); err != nil {
		fmt.Fprintf(cfg.Stderr, "wasi: %v\n", err)
		return 126
	}
	defer host.closeAll()

	_, err = runtime.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName(""))
	var exit *sys.ExitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &exit):
		if ctx.Err() != nil {
			return 130 // stopped by Ctrl-C: 128 + SIGINT
		}
		return int(exit.ExitCode())
	default:
		fmt.Fprintf(cfg.Stderr, "wasi: %v\n", err)
		return 1
	}
}
