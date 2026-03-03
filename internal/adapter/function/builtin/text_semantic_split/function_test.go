package textsemanticsplit

import (
	"context"
	"strings"
	"testing"
)

func TestSemanticSplitSuccess(t *testing.T) {
	f := &function{}
	text := strings.Repeat("x", 2400)
	out, err := f.Execute(context.Background(), map[string]interface{}{
		"args": map[string]interface{}{
			"text": text,
			"chunk_size": 180,
			"overlap":    20,
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	chunks, ok := out["chunks"].([]string)
	if !ok {
		t.Fatalf("expected chunks []string, got %T", out["chunks"])
	}
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}
	if out["chunk_count"] != len(chunks) {
		t.Fatalf("chunk_count mismatch: %v vs %d", out["chunk_count"], len(chunks))
	}
}

func TestSemanticSplitMissingArgs(t *testing.T) {
	f := &function{}
	_, err := f.Execute(context.Background(), map[string]interface{}{})
	if err == nil {
		t.Fatal("expected missing args error")
	}
}

func TestSemanticSplitMissingText(t *testing.T) {
	f := &function{}
	_, err := f.Execute(context.Background(), map[string]interface{}{
		"args": map[string]interface{}{},
	})
	if err == nil {
		t.Fatal("expected missing text error")
	}
}

func TestSemanticSplitFromCurrentTextObject(t *testing.T) {
	f := &function{}
	out, err := f.Execute(context.Background(), map[string]interface{}{
		"args": map[string]interface{}{
			"current_text": map[string]interface{}{"text": strings.Repeat("y", 1800)},
			"chunk_size":   300,
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out["chunk_count"].(int) < 2 {
		t.Fatalf("expected chunk_count >= 2, got %v", out["chunk_count"])
	}
}
