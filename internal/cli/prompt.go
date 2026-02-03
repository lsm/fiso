package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

type prompter struct {
	scanner *bufio.Scanner
	out     io.Writer
}

func newPrompter(in io.Reader, out io.Writer) *prompter {
	if in == nil {
		in = os.Stdin
	}
	if out == nil {
		out = os.Stdout
	}
	return &prompter{scanner: bufio.NewScanner(in), out: out}
}

// choose displays a numbered multiple-choice prompt and returns the 0-based index
// of the selected option. defaultIdx is the 0-based index used when the user
// presses Enter without typing anything or on EOF.
func (p *prompter) choose(question string, options []string, defaultIdx int) int {
	_, _ = fmt.Fprintf(p.out, "\n%s\n", question)
	for i, opt := range options {
		marker := "  "
		if i == defaultIdx {
			marker = "▸ "
		}
		_, _ = fmt.Fprintf(p.out, "  %s%d) %s\n", marker, i+1, opt)
	}
	_, _ = fmt.Fprintf(p.out, "  Choose [%d]: ", defaultIdx+1)

	if !p.scanner.Scan() {
		return defaultIdx
	}
	text := strings.TrimSpace(p.scanner.Text())
	if text == "" {
		return defaultIdx
	}
	n, err := strconv.Atoi(text)
	if err != nil || n < 1 || n > len(options) {
		// Invalid input, use default
		return defaultIdx
	}
	return n - 1
}

// confirm displays a yes/no prompt. Returns defaultYes on empty input or EOF.
func (p *prompter) confirm(question string, defaultYes bool) bool {
	hint := "[y/N]"
	if defaultYes {
		hint = "[Y/n]"
	}
	_, _ = fmt.Fprintf(p.out, "\n%s %s: ", question, hint)

	if !p.scanner.Scan() {
		return defaultYes
	}
	text := strings.TrimSpace(strings.ToLower(p.scanner.Text()))
	if text == "" {
		return defaultYes
	}
	switch text {
	case "y", "yes":
		return true
	case "n", "no":
		return false
	default:
		return defaultYes
	}
}
