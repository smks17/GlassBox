package wasi

import (
	"context"
	"fmt"
	"io"

	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental"
)

type tracer struct{ w io.Writer }

func (t tracer) NewFunctionListener(def api.FunctionDefinition) experimental.FunctionListener {
	// The listener is attached when a module is compiled, so a system call is
	// seen from the host module's side: a Go function, not an import.
	if def.GoFunction() != nil {
		return &call{w: t.w}
	}
	return nil // the guest's own functions: far too many to print
}

// call remembers the arguments of the call in flight, so After can print
// them on one line with the result.
type call struct {
	w      io.Writer
	params []uint64
}

func (c *call) Before(_ context.Context, _ api.Module, def api.FunctionDefinition, params []uint64, _ experimental.StackIterator) {
	c.params = append(c.params[:0], params[:len(def.ParamTypes())]...)
}

func (c *call) After(_ context.Context, _ api.Module, def api.FunctionDefinition, results []uint64) {
	line := "wasi: " + def.Name() + "("
	for i, p := range c.params {
		if i > 0 {
			line += ", "
		}
		line += fmt.Sprint(p)
	}
	line += ")"
	if len(results) > 0 {
		line += fmt.Sprintf(" = %d", results[0])
	}
	fmt.Fprintln(c.w, line)
}

func (c *call) Abort(_ context.Context, _ api.Module, def api.FunctionDefinition, err error) {
	fmt.Fprintf(c.w, "wasi: %s aborted: %v\n", def.Name(), err)
}
