package echo

import (
	"context"

	"flowweave/internal/domain/workflow/node/code"
)

type function struct{}

func (f *function) Name() string {
	return "flowweave.echo.v1"
}

func (f *function) Execute(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
	return map[string]interface{}{
		"result": input,
	}, nil
}

func init() {
	code.MustRegisterFunction(&function{})
}
