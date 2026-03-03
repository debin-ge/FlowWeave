package primejudge

import (
	"context"
	"fmt"
	"math"

	"flowweave/internal/domain/workflow/node/code"
)

type function struct{}

func (f *function) Name() string {
	return "math.prime_judge.v1"
}

func (f *function) Execute(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
	n, err := extractNumber(input)
	if err != nil {
		return nil, err
	}

	isPrime := isPrimeNumber(n)
	next := n
	if !isPrime {
		next = n + 1
	}

	return map[string]interface{}{
		"is_prime":      isPrime,
		"continue_loop": !isPrime,
		"next_number":   next,
	}, nil
}

func extractNumber(input map[string]interface{}) (int64, error) {
	if v, ok := input["number"]; ok {
		return toInt64(v)
	}
	if rawArgs, ok := input["args"]; ok {
		args, ok := rawArgs.(map[string]interface{})
		if !ok {
			return 0, fmt.Errorf("input args must be object, got %T", rawArgs)
		}
		v, ok := args["number"]
		if !ok {
			return 0, fmt.Errorf("missing required field: number")
		}
		return toInt64(v)
	}
	return 0, fmt.Errorf("missing required input: number (or args.number)")
}

func toInt64(v interface{}) (int64, error) {
	switch n := v.(type) {
	case int:
		return int64(n), nil
	case int8:
		return int64(n), nil
	case int16:
		return int64(n), nil
	case int32:
		return int64(n), nil
	case int64:
		return n, nil
	case uint:
		return int64(n), nil
	case uint8:
		return int64(n), nil
	case uint16:
		return int64(n), nil
	case uint32:
		return int64(n), nil
	case uint64:
		return int64(n), nil
	case float32:
		return int64(n), nil
	case float64:
		return int64(n), nil
	default:
		return 0, fmt.Errorf("number must be numeric, got %T", v)
	}
}

func isPrimeNumber(n int64) bool {
	if n <= 1 {
		return false
	}
	if n == 2 {
		return true
	}
	if n%2 == 0 {
		return false
	}

	limit := int64(math.Sqrt(float64(n)))
	for i := int64(3); i <= limit; i += 2 {
		if n%i == 0 {
			return false
		}
	}
	return true
}

func init() {
	code.MustRegisterFunction(&function{})
}
