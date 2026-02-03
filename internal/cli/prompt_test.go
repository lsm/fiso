package cli

import (
	"bufio"
	"bytes"
	"io"
	"os"
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

// testInteractivePrompter creates a prompter for testing interactive mode
func testInteractivePrompter(input string) (*prompter, *bytes.Buffer) {
	r, w, _ := os.Pipe()
	go func() {
		_, _ = w.WriteString(input)
		_ = w.Close()
	}()
	var out bytes.Buffer
	return &prompter{
		in:      r,
		scanner: bufio.NewScanner(r),
		out:     &out,
		isTTY:   true,
		inFd:    -1, // not used by interactiveChooseRaw
	}, &out
}

func TestInteractiveChoose_EnterWithNoNavigation(t *testing.T) {
	p, out := testInteractivePrompter("\r")
	idx := p.interactiveChooseRaw("Pick:", []string{"A", "B", "C"}, 1)
	if idx != 1 {
		t.Errorf("expected 1 (default), got %d", idx)
	}
	// Verify output contains ANSI color codes
	if !strings.Contains(out.String(), ansiCyan) {
		t.Error("expected output to contain cyan color code")
	}
}

func TestInteractiveChoose_DownArrowThenEnter(t *testing.T) {
	p, _ := testInteractivePrompter("\x1b[B\r")
	idx := p.interactiveChooseRaw("Pick:", []string{"A", "B", "C"}, 0)
	if idx != 1 {
		t.Errorf("expected 1 (defaultIdx+1), got %d", idx)
	}
}

func TestInteractiveChoose_UpArrowAtTop(t *testing.T) {
	p, _ := testInteractivePrompter("\x1b[A\r")
	idx := p.interactiveChooseRaw("Pick:", []string{"A", "B", "C"}, 0)
	if idx != 0 {
		t.Errorf("expected 0 (stays at top), got %d", idx)
	}
}

func TestInteractiveChoose_VimJKey(t *testing.T) {
	p, _ := testInteractivePrompter("j\r")
	idx := p.interactiveChooseRaw("Pick:", []string{"A", "B", "C"}, 0)
	if idx != 1 {
		t.Errorf("expected 1 (moved down), got %d", idx)
	}
}

func TestInteractiveChoose_VimKKeyAtTop(t *testing.T) {
	p, _ := testInteractivePrompter("k\r")
	idx := p.interactiveChooseRaw("Pick:", []string{"A", "B", "C"}, 0)
	if idx != 0 {
		t.Errorf("expected 0 (stays at top), got %d", idx)
	}
}

func TestInteractiveChoose_NumberKeyAutoConfirms(t *testing.T) {
	p, _ := testInteractivePrompter("2")
	idx := p.interactiveChooseRaw("Pick:", []string{"A", "B", "C"}, 0)
	if idx != 1 {
		t.Errorf("expected 1 (index of option 2), got %d", idx)
	}
}

func TestInteractiveChoose_OutputContainsCyan(t *testing.T) {
	p, out := testInteractivePrompter("\r")
	p.interactiveChooseRaw("Pick:", []string{"A", "B"}, 0)
	output := out.String()
	if !strings.Contains(output, ansiCyan) {
		t.Error("expected output to contain cyan color code for selected option")
	}
}

func TestInteractiveChoose_MultipleDownArrows(t *testing.T) {
	p, _ := testInteractivePrompter("\x1b[B\x1b[B\r")
	idx := p.interactiveChooseRaw("Pick:", []string{"A", "B", "C"}, 0)
	if idx != 2 {
		t.Errorf("expected 2 (moved down twice), got %d", idx)
	}
}

func TestInteractiveChoose_DownThenUpArrow(t *testing.T) {
	p, _ := testInteractivePrompter("\x1b[B\x1b[A\r")
	idx := p.interactiveChooseRaw("Pick:", []string{"A", "B", "C"}, 0)
	if idx != 0 {
		t.Errorf("expected 0 (down then up), got %d", idx)
	}
}

func TestInteractiveChoose_DownArrowAtBottom(t *testing.T) {
	p, _ := testInteractivePrompter("\x1b[B\x1b[B\x1b[B\r")
	idx := p.interactiveChooseRaw("Pick:", []string{"A", "B"}, 0)
	if idx != 1 {
		t.Errorf("expected 1 (stays at bottom), got %d", idx)
	}
}

func TestInteractiveChoose_VimJKNavigation(t *testing.T) {
	p, _ := testInteractivePrompter("jjk\r")
	idx := p.interactiveChooseRaw("Pick:", []string{"A", "B", "C"}, 0)
	if idx != 1 {
		t.Errorf("expected 1 (j,j,k navigation), got %d", idx)
	}
}

func TestInteractiveChoose_NumberKey3(t *testing.T) {
	p, _ := testInteractivePrompter("3")
	idx := p.interactiveChooseRaw("Pick:", []string{"A", "B", "C"}, 0)
	if idx != 2 {
		t.Errorf("expected 2 (index of option 3), got %d", idx)
	}
}
