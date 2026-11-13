package wasi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"parrot/internal/engine/filesystem"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental"
	"github.com/tetratelabs/wazero/sys"
)

const (
	errnoSuccess = 0
	errnoNosys   = 52
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

func instantiateHost(ctx context.Context, runtime wazero.Runtime, compiled wazero.CompiledModule) error {
	const module = "wasi_snapshot_preview1"
	calls := map[string]any{
		// The runner has no other guest threads to schedule, so yielding succeeds immediately.
		"sched_yield": func() uint32 { return errnoSuccess },
	}

	builder := runtime.NewHostModuleBuilder(module)
	for name, fn := range calls {
		builder.NewFunctionBuilder().WithFunc(fn).Export(name)
	}
	builder.NewFunctionBuilder().WithGoModuleFunction(api.GoModuleFunc(procExit), []api.ValueType{api.ValueTypeI32}, nil).Export("proc_exit")

	for _, def := range compiled.ImportedFunctions() {
		mod, name, _ := def.Import()
		if _, ok := calls[name]; mod != module || ok || name == "proc_exit" {
			continue
		}
		// builder.NewFunctionBuilder().
		// 	WithGoModuleFunction(enosys(name), def.ParamTypes(), def.ResultTypes()).
		// 	Export(name)
	}

	_, err := builder.Instantiate(ctx)
	return err
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

	if err := instantiateHost(ctx, runtime, compiled); err != nil {
		fmt.Fprintf(cfg.Stderr, "wasi: %v\n", err)
		return 126
	}

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

func enosys(wasiContext WasiContext, name string) api.GoModuleFunc {
	return func(_ context.Context, _ api.Module, stack []uint64) {
		fmt.Fprintf(wasiContext.Stderr, "wasi: %s not implemented\n", name)
		stack[0] = errnoNosys
	}
}
