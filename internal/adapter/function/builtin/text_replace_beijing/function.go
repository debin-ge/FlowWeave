package textreplacebeijing

import (
	"context"
	"fmt"
	"strings"

	"flowweave/internal/domain/workflow/node/code"
)

type function struct{}

func (f *function) Name() string {
	return "text.replace_beijing.v1"
}

func (f *function) Execute(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
	raw, ok := input["text"]
	if !ok {
		return nil, fmt.Errorf("missing required input: text")
	}
	text, ok := raw.(string)
	if !ok {
		return nil, fmt.Errorf("input text must be string, got %T", raw)
	}

	return map[string]interface{}{
		"result": strings.ReplaceAll(text, "北京", "南京"),
	}, nil
}

func init() {
	code.MustRegisterFunction(&function{})
}
