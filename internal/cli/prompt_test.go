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

func TestNewPrompter_NonFile(t *testing.T) {
	// Create a prompter with strings.NewReader (not an *os.File)
	r := strings.NewReader("test input")
	p := newPrompter(r, io.Discard)

	if p.isTTY {
		t.Error("expected isTTY to be false for non-file input")
	}
	if p.inFd != -1 {
		t.Errorf("expected inFd to be -1 for non-file input, got %d", p.inFd)
	}
	if p.in != r {
		t.Error("expected input reader to be set")
	}
}

func TestInteractiveChoose_LoneEsc(t *testing.T) {
	// Send ESC (0x1b) followed by Enter (0x0d) - should select default option
	p, _ := testInteractivePrompter("\x1b\r")
	idx := p.interactiveChooseRaw("Pick:", []string{"A", "B", "C"}, 1)
	if idx != 1 {
		t.Errorf("expected 1 (default after lone ESC), got %d", idx)
	}
}

func TestInteractiveChoose_EOF(t *testing.T) {
	// Send empty input to cause EOF
	p, _ := testInteractivePrompter("")
	idx := p.interactiveChooseRaw("Pick:", []string{"A", "B", "C"}, 2)
	if idx != 2 {
		t.Errorf("expected 2 (default on EOF), got %d", idx)
	}
}

func TestInteractiveChoose_NewlineKey(t *testing.T) {
	// Send \n instead of \r to trigger selection
	p, _ := testInteractivePrompter("\n")
	idx := p.interactiveChooseRaw("Pick:", []string{"A", "B", "C"}, 0)
	if idx != 0 {
		t.Errorf("expected 0 (newline should select), got %d", idx)
	}
}

func TestChoose_NonTTY_UsesNumberedChoice(t *testing.T) {
	// Create a non-TTY prompter (using strings.NewReader)
	p := newPrompter(strings.NewReader("2\n"), io.Discard)
	if p.isTTY {
		t.Fatal("expected isTTY to be false")
	}

	idx := p.choose("Pick:", []string{"A", "B", "C"}, 0)
	if idx != 1 {
		t.Errorf("expected 1 (option 2), got %d", idx)
	}
}

func TestChoose_TTY_UsesInteractiveChoice(t *testing.T) {
	// Create a TTY prompter that should use interactive choice
	r, w, _ := os.Pipe()
	go func() {
		_, _ = w.WriteString("2\n")
		_ = w.Close()
	}()
	var out bytes.Buffer
	p := &prompter{
		in:      r,
		scanner: bufio.NewScanner(r),
		out:     &out,
		isTTY:   true,
		inFd:    -1, // Invalid fd will make it fall back to numberedChoose
	}

	// This should attempt interactive but fall back to numbered
	idx := p.choose("Pick:", []string{"A", "B", "C"}, 0)
	if idx != 1 {
		t.Errorf("expected 1 (option 2), got %d", idx)
	}
}

func TestConfirm_TTY_Formatting(t *testing.T) {
	// Create a TTY prompter
	r, w, _ := os.Pipe()
	go func() {
		_, _ = w.WriteString("y\n")
		_ = w.Close()
	}()
	var out bytes.Buffer
	p := &prompter{
		in:      r,
		scanner: bufio.NewScanner(r),
		out:     &out,
		isTTY:   true,
		inFd:    -1,
	}

	result := p.confirm("Enable?", false)
	if !result {
		t.Error("expected true for 'y' input")
	}

	// Verify output contains ANSI codes for TTY
	if !strings.Contains(out.String(), ansiBold) {
		t.Error("expected output to contain bold ANSI code for TTY")
	}
}

func TestConfirm_NonTTY_NoFormatting(t *testing.T) {
	p := newPrompter(strings.NewReader("n\n"), io.Discard)
	if p.isTTY {
		t.Fatal("expected isTTY to be false")
	}

	result := p.confirm("Enable?", true)
	if result {
		t.Error("expected false for 'n' input")
	}
}

func TestNewPrompter_NilInputOutput(t *testing.T) {
	// Test that nil input/output defaults to stdin/stdout
	p := newPrompter(nil, nil)
	if p.in != os.Stdin {
		t.Error("expected in to be os.Stdin when nil is passed")
	}
	if p.out != os.Stdout {
		t.Error("expected out to be os.Stdout when nil is passed")
	}
}

func TestInteractiveChoose_FallbackOnRawError(t *testing.T) {
	// Create a prompter with an invalid file descriptor to trigger the MakeRaw error path
	// This tests the fallback to numberedChoose when interactive mode can't be established
	r, w, _ := os.Pipe()
	go func() {
		_, _ = w.WriteString("2\n")
		_ = w.Close()
	}()
	var out bytes.Buffer
	p := &prompter{
		in:      r,
		scanner: bufio.NewScanner(r),
		out:     &out,
		isTTY:   true,
		inFd:    -1, // Invalid fd to force MakeRaw to fail
	}

	// This should fall back to numberedChoose
	idx := p.interactiveChoose("Pick:", []string{"A", "B", "C"}, 0)
	if idx != 1 {
		t.Errorf("expected 1 (option 2), got %d", idx)
	}
}

func TestRenderOptions(t *testing.T) {
	var out bytes.Buffer
	p := &prompter{out: &out}
	p.renderOptions([]string{"Option A", "Option B", "Option C"}, 1)

	output := out.String()
	if !strings.Contains(output, "Option A") || !strings.Contains(output, "Option B") || !strings.Contains(output, "Option C") {
		t.Error("expected all options to be rendered")
	}
	if !strings.Contains(output, ansiCyan) {
		t.Error("expected selected option to have cyan color")
	}
}

func TestClearOptions(t *testing.T) {
	var out bytes.Buffer
	p := &prompter{out: &out}
	p.clearOptions(3)

	output := out.String()
	if !strings.Contains(output, "\033[A") {
		t.Error("expected cursor movement ANSI codes")
	}
}
