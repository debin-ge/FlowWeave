package summaryjudge

import (
	"context"
	"strings"
	"testing"
)

func TestSummaryJudgeOverLimit(t *testing.T) {
	f := &function{}
	text := strings.Repeat("这是一个非常长的文本片段。", 200)
	out, err := f.Execute(context.Background(), map[string]interface{}{
		"args": map[string]interface{}{
			"text":            text,
			"context_window":  1000,
			"reserve_tokens":  100,
			"threshold_ratio": 0.8,
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	over, ok := out["over_limit"].(bool)
	if !ok {
		t.Fatalf("expected over_limit bool, got %T", out["over_limit"])
	}
	if !over {
		t.Fatalf("expected over_limit=true, output=%v", out)
	}
}

func TestSummaryJudgeUnderLimit(t *testing.T) {
	f := &function{}
	out, err := f.Execute(context.Background(), map[string]interface{}{
		"args": map[string]interface{}{
			"text": "short text",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	over, ok := out["over_limit"].(bool)
	if !ok {
		t.Fatalf("expected over_limit bool, got %T", out["over_limit"])
	}
	if over {
		t.Fatalf("expected over_limit=false, output=%v", out)
	}
}

func TestSummaryJudgeOverLimitWithTinyBudget(t *testing.T) {
	f := &function{}
	out, err := f.Execute(context.Background(), map[string]interface{}{
		"args": map[string]interface{}{
			"text":            "short text",
			"context_window":  10,
			"reserve_tokens":  9,
			"threshold_ratio": 0.8,
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	over, ok := out["over_limit"].(bool)
	if !ok {
		t.Fatalf("expected over_limit bool, got %T", out["over_limit"])
	}
	if !over {
		t.Fatalf("expected over_limit=true, output=%v", out)
	}
}

func TestSummaryJudgeMissingArgs(t *testing.T) {
	f := &function{}
	_, err := f.Execute(context.Background(), map[string]interface{}{})
	if err == nil {
		t.Fatal("expected missing args error")
	}
}

func TestSummaryJudgeFromCurrentTextString(t *testing.T) {
	f := &function{}
	out, err := f.Execute(context.Background(), map[string]interface{}{
		"args": map[string]interface{}{
			"current_text":   strings.Repeat("a", 200),
			"context_window": 200,
			"reserve_tokens": 10,
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, ok := out["over_limit"].(bool)
	if !ok {
		t.Fatalf("expected bool over_limit, got %T", out["over_limit"])
	}
}
