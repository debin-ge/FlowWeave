package summaryjudge

import (
	"context"
	"fmt"
	"strings"

	"flowweave/internal/domain/memory"
	"flowweave/internal/domain/workflow/node/code"
)

const (
	defaultContextWindow = 1000
	defaultReserveTokens = 1000
	defaultThresholdRate = 0.80
)

type function struct{}

func (f *function) Name() string {
	return "summary.judge.v1"
}

func (f *function) Execute(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
	args, err := parseArgs(input)
	if err != nil {
		return nil, err
	}

	text, err := extractText(args)
	if err != nil {
		return nil, err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fmt.Errorf("args.text must not be empty")
	}

	contextWindow := getInt(args, "context_window", defaultContextWindow)
	reserveTokens := getInt(args, "reserve_tokens", defaultReserveTokens)
	thresholdRatio := getFloat(args, "threshold_ratio", defaultThresholdRate)

	if contextWindow < 1 {
		contextWindow = defaultContextWindow
	}
	if reserveTokens < 0 {
		reserveTokens = 0
	}
	if thresholdRatio <= 0 || thresholdRatio > 1 {
		thresholdRatio = defaultThresholdRate
	}

	rawBudget := contextWindow - reserveTokens
	if rawBudget < 1 {
		rawBudget = 1
	}
	budget := int(float64(rawBudget) * thresholdRatio)
	if budget < 1 {
		budget = 1
	}

	estimator := &memory.SimpleTokenEstimator{}
	tokenEstimate := estimator.EstimateTokens(text)
	overLimit := tokenEstimate > budget

	reason := "under_budget"
	if overLimit {
		reason = "exceeds_budget"
	}

	return map[string]interface{}{
		"over_limit":     overLimit,
		"token_estimate": tokenEstimate,
		"budget":         budget,
		"reason":         reason,
		"detail": map[string]interface{}{
			"context_window":  contextWindow,
			"reserve_tokens":  reserveTokens,
			"threshold_ratio": thresholdRatio,
			"raw_budget":      rawBudget,
		},
	}, nil
}

func parseArgs(input map[string]interface{}) (map[string]interface{}, error) {
	raw, ok := input["args"]
	if !ok {
		return nil, fmt.Errorf("missing required input: args")
	}
	args, ok := raw.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("input args must be object, got %T", raw)
	}
	return args, nil
}

func getRequiredString(args map[string]interface{}, key string) (string, error) {
	raw, ok := args[key]
	if !ok {
		return "", fmt.Errorf("missing required args field: %s", key)
	}
	val, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("args.%s must be string, got %T", key, raw)
	}
	return val, nil
}

func extractText(args map[string]interface{}) (string, error) {
	if text, err := getRequiredString(args, "text"); err == nil {
		return text, nil
	}

	raw, ok := args["current_text"]
	if !ok {
		return "", fmt.Errorf("missing required args field: text (or current_text)")
	}

	switch v := raw.(type) {
	case string:
		return v, nil
	case map[string]interface{}:
		if s, ok := v["text"].(string); ok {
			return s, nil
		}
		return "", fmt.Errorf("args.current_text object must contain string field 'text'")
	default:
		return "", fmt.Errorf("args.current_text must be string or object, got %T", raw)
	}
}

func getInt(args map[string]interface{}, key string, def int) int {
	raw, ok := args[key]
	if !ok {
		return def
	}
	switch v := raw.(type) {
	case int:
		return v
	case int8:
		return int(v)
	case int16:
		return int(v)
	case int32:
		return int(v)
	case int64:
		return int(v)
	case uint:
		return int(v)
	case uint8:
		return int(v)
	case uint16:
		return int(v)
	case uint32:
		return int(v)
	case uint64:
		return int(v)
	case float32:
		return int(v)
	case float64:
		return int(v)
	default:
		return def
	}
}

func getFloat(args map[string]interface{}, key string, def float64) float64 {
	raw, ok := args[key]
	if !ok {
		return def
	}
	switch v := raw.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int8:
		return float64(v)
	case int16:
		return float64(v)
	case int32:
		return float64(v)
	case int64:
		return float64(v)
	default:
		return def
	}
}

func init() {
	code.MustRegisterFunction(&function{})
}
