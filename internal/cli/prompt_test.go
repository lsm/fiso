package cli

import (
	"io"
	"strings"
	"testing"
)

func TestChoose_DefaultOnEmpty(t *testing.T) {
	p := newPrompter(strings.NewReader("\n"), io.Discard)
	idx := p.choose("Pick:", []string{"A", "B"}, 0)
	if idx != 0 {
		t.Errorf("expected 0, got %d", idx)
	}
}

func TestChoose_DefaultOnEOF(t *testing.T) {
	p := newPrompter(strings.NewReader(""), io.Discard)
	idx := p.choose("Pick:", []string{"A", "B"}, 1)
	if idx != 1 {
		t.Errorf("expected 1, got %d", idx)
	}
}

func TestChoose_ExplicitSelection(t *testing.T) {
	p := newPrompter(strings.NewReader("2\n"), io.Discard)
	idx := p.choose("Pick:", []string{"A", "B", "C"}, 0)
	if idx != 1 {
		t.Errorf("expected 1, got %d", idx)
	}
}

func TestChoose_FirstOption(t *testing.T) {
	p := newPrompter(strings.NewReader("1\n"), io.Discard)
	idx := p.choose("Pick:", []string{"A", "B"}, 1)
	if idx != 0 {
		t.Errorf("expected 0, got %d", idx)
	}
}

func TestChoose_InvalidInputUsesDefault(t *testing.T) {
	p := newPrompter(strings.NewReader("abc\n"), io.Discard)
	idx := p.choose("Pick:", []string{"A", "B"}, 0)
	if idx != 0 {
		t.Errorf("expected 0, got %d", idx)
	}
}

func TestChoose_OutOfRangeUsesDefault(t *testing.T) {
	p := newPrompter(strings.NewReader("5\n"), io.Discard)
	idx := p.choose("Pick:", []string{"A", "B"}, 0)
	if idx != 0 {
		t.Errorf("expected 0, got %d", idx)
	}
}

func TestChoose_ZeroUsesDefault(t *testing.T) {
	p := newPrompter(strings.NewReader("0\n"), io.Discard)
	idx := p.choose("Pick:", []string{"A", "B"}, 1)
	if idx != 1 {
		t.Errorf("expected 1, got %d", idx)
	}
}

func TestConfirm_DefaultNo(t *testing.T) {
	p := newPrompter(strings.NewReader("\n"), io.Discard)
	result := p.confirm("Enable?", false)
	if result {
		t.Error("expected false")
	}
}

func TestConfirm_DefaultYes(t *testing.T) {
	p := newPrompter(strings.NewReader("\n"), io.Discard)
	result := p.confirm("Enable?", true)
	if !result {
		t.Error("expected true")
	}
}

func TestConfirm_ExplicitYes(t *testing.T) {
	p := newPrompter(strings.NewReader("y\n"), io.Discard)
	result := p.confirm("Enable?", false)
	if !result {
		t.Error("expected true")
	}
}

func TestConfirm_ExplicitNo(t *testing.T) {
	p := newPrompter(strings.NewReader("no\n"), io.Discard)
	result := p.confirm("Enable?", true)
	if result {
		t.Error("expected false")
	}
}

func TestConfirm_EOFUsesDefault(t *testing.T) {
	p := newPrompter(strings.NewReader(""), io.Discard)
	result := p.confirm("Enable?", true)
	if !result {
		t.Error("expected true on EOF with defaultYes=true")
	}
}

func TestConfirm_InvalidInputUsesDefault(t *testing.T) {
	p := newPrompter(strings.NewReader("maybe\n"), io.Discard)
	result := p.confirm("Enable?", false)
	if result {
		t.Error("expected false for invalid input with defaultYes=false")
	}
}

func TestChoose_MultiplePrompts(t *testing.T) {
	// Simulate answering multiple prompts in sequence
	input := "2\n1\n3\n"
	p := newPrompter(strings.NewReader(input), io.Discard)

	idx1 := p.choose("Q1:", []string{"A", "B"}, 0)
	if idx1 != 1 {
		t.Errorf("Q1: expected 1, got %d", idx1)
	}

	idx2 := p.choose("Q2:", []string{"X", "Y"}, 1)
	if idx2 != 0 {
		t.Errorf("Q2: expected 0, got %d", idx2)
	}

	idx3 := p.choose("Q3:", []string{"P", "Q", "R"}, 0)
	if idx3 != 2 {
		t.Errorf("Q3: expected 2, got %d", idx3)
	}
}
