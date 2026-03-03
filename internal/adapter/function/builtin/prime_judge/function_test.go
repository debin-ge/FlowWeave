package primejudge

import (
	"context"
	"testing"
)

func TestPrimeJudgePrime(t *testing.T) {
	f := &function{}
	out, err := f.Execute(context.Background(), map[string]interface{}{"number": 11})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out["is_prime"] != true {
		t.Fatalf("expected is_prime=true, got %v", out["is_prime"])
	}
	if out["continue_loop"] != false {
		t.Fatalf("expected continue_loop=false, got %v", out["continue_loop"])
	}
	if out["next_number"] != int64(11) {
		t.Fatalf("expected next_number=11, got %v", out["next_number"])
	}
}

func TestPrimeJudgeNonPrime(t *testing.T) {
	f := &function{}
	out, err := f.Execute(context.Background(), map[string]interface{}{"args": map[string]interface{}{"number": 8}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out["is_prime"] != false {
		t.Fatalf("expected is_prime=false, got %v", out["is_prime"])
	}
	if out["continue_loop"] != true {
		t.Fatalf("expected continue_loop=true, got %v", out["continue_loop"])
	}
	if out["next_number"] != int64(9) {
		t.Fatalf("expected next_number=9, got %v", out["next_number"])
	}
}

func TestPrimeJudgeMissingInput(t *testing.T) {
	f := &function{}
	_, err := f.Execute(context.Background(), map[string]interface{}{})
	if err == nil {
		t.Fatal("expected error for missing number")
	}
}
