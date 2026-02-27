package code

import (
	"context"
	"fmt"
	"sync"
)

// LocalFunction is a callable local function for Code node.
type LocalFunction interface {
	Name() string
	Execute(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error)
}

type functionRegistry struct {
	mu    sync.RWMutex
	funcs map[string]LocalFunction
}

func newFunctionRegistry() *functionRegistry {
	return &functionRegistry{
		funcs: make(map[string]LocalFunction),
	}
}

func (r *functionRegistry) Register(fn LocalFunction) error {
	if fn == nil {
		return fmt.Errorf("function is nil")
	}
	name := fn.Name()
	if name == "" {
		return fmt.Errorf("function name is empty")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.funcs[name]; exists {
		return fmt.Errorf("function already registered: %s", name)
	}
	r.funcs[name] = fn
	return nil
}

func (r *functionRegistry) Get(name string) (LocalFunction, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	fn, ok := r.funcs[name]
	return fn, ok
}

var globalFunctionRegistry = newFunctionRegistry()

func RegisterFunction(fn LocalFunction) error {
	return globalFunctionRegistry.Register(fn)
}

func MustRegisterFunction(fn LocalFunction) {
	if err := RegisterFunction(fn); err != nil {
		panic(err)
	}
}

func GetFunction(name string) (LocalFunction, bool) {
	return globalFunctionRegistry.Get(name)
}
