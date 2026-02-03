package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"golang.org/x/term"
)

const (
	ansiReset      = "\033[0m"
	ansiCyan       = "\033[36m"
	ansiBold       = "\033[1m"
	ansiDim        = "\033[2m"
	ansiGreen      = "\033[32m"
	ansiHideCursor = "\033[?25l"
	ansiShowCursor = "\033[?25h"
	ansiClearLine  = "\033[2K"
)

type prompter struct {
	in      io.Reader
	scanner *bufio.Scanner
	out     io.Writer
	isTTY   bool
	inFd    int
}

func newPrompter(in io.Reader, out io.Writer) *prompter {
	if in == nil {
		in = os.Stdin
	}
	if out == nil {
		out = os.Stdout
	}

	isTTY := false
	inFd := -1
	if f, ok := in.(*os.File); ok {
		fd := int(f.Fd())
		if term.IsTerminal(fd) {
			isTTY = true
			inFd = fd
		}
	}

	return &prompter{
		in:      in,
		scanner: bufio.NewScanner(in),
		out:     out,
		isTTY:   isTTY,
		inFd:    inFd,
	}
}

// choose displays a numbered multiple-choice prompt and returns the 0-based index
// of the selected option. defaultIdx is the 0-based index used when the user
// presses Enter without typing anything or on EOF.
func (p *prompter) choose(question string, options []string, defaultIdx int) int {
	if p.isTTY {
		return p.interactiveChoose(question, options, defaultIdx)
	}
	return p.numberedChoose(question, options, defaultIdx)
}

// numberedChoose is the original numbered choice behavior, preserved exactly for tests
func (p *prompter) numberedChoose(question string, options []string, defaultIdx int) int {
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

// interactiveChoose is the TTY-aware arrow-key navigation path
func (p *prompter) interactiveChoose(question string, options []string, defaultIdx int) int {
	oldState, err := term.MakeRaw(p.inFd)
	if err != nil {
		return p.numberedChoose(question, options, defaultIdx)
	}
	defer func() { _ = term.Restore(p.inFd, oldState) }()
	return p.interactiveChooseRaw(question, options, defaultIdx)
}

// interactiveChooseRaw implements the core rendering and input loop
// Caller must handle raw mode setup/teardown
func (p *prompter) interactiveChooseRaw(question string, options []string, defaultIdx int) int {
	selected := defaultIdx

	// Print question in bold
	_, _ = fmt.Fprintf(p.out, "\r\n%s%s%s\r\n", ansiBold, question, ansiReset)

	// Render initial options
	p.renderOptions(options, selected)

	// Hide cursor
	_, _ = fmt.Fprintf(p.out, "%s", ansiHideCursor)
	defer func() { _, _ = fmt.Fprintf(p.out, "%s", ansiShowCursor) }()

	// Input loop
	buf := make([]byte, 3)
	for {
		n, err := p.in.Read(buf)
		if err != nil {
			break
		}

		if n == 0 {
			continue
		}

		// Check for Enter
		if buf[0] == '\r' || buf[0] == '\n' {
			// Clear options
			p.clearOptions(len(options))
			// Print final selection in green
			_, _ = fmt.Fprintf(p.out, "  %s▸ %s%s\r\n", ansiGreen, options[selected], ansiReset)
			return selected
		}

		// Check for Ctrl-C
		if buf[0] == 3 {
			_, _ = fmt.Fprintf(p.out, "%s", ansiShowCursor)
			os.Exit(130)
		}

		// Check for escape sequences (arrow keys)
		if n >= 3 && buf[0] == 0x1b && buf[1] == '[' {
			switch buf[2] {
			case 'A': // Up arrow
				if selected > 0 {
					selected--
					p.clearOptions(len(options))
					p.renderOptions(options, selected)
				}
			case 'B': // Down arrow
				if selected < len(options)-1 {
					selected++
					p.clearOptions(len(options))
					p.renderOptions(options, selected)
				}
			}
			continue
		}

		// Check for vim keys
		if buf[0] == 'k' {
			if selected > 0 {
				selected--
				p.clearOptions(len(options))
				p.renderOptions(options, selected)
			}
			continue
		}

		if buf[0] == 'j' {
			if selected < len(options)-1 {
				selected++
				p.clearOptions(len(options))
				p.renderOptions(options, selected)
			}
			continue
		}

		// Check for direct number selection (1-9)
		if buf[0] >= '1' && buf[0] <= '9' {
			num := int(buf[0] - '0')
			if num <= len(options) {
				selected = num - 1
				// Clear options
				p.clearOptions(len(options))
				// Print final selection in green
				_, _ = fmt.Fprintf(p.out, "  %s▸ %s%s\r\n", ansiGreen, options[selected], ansiReset)
				return selected
			}
		}

		// Ignore lone ESC (byte 27) if not part of arrow sequence
		if buf[0] == 0x1b && n == 1 {
			continue
		}
	}

	return selected
}

// renderOptions displays the list of options with the selected one highlighted
func (p *prompter) renderOptions(options []string, selected int) {
	for i, opt := range options {
		if i == selected {
			_, _ = fmt.Fprintf(p.out, "  %s▸ %s%s\r\n", ansiCyan, opt, ansiReset)
		} else {
			_, _ = fmt.Fprintf(p.out, "  %s  %s%s\r\n", ansiDim, opt, ansiReset)
		}
	}
}

// clearOptions moves cursor up and clears the specified number of option lines
func (p *prompter) clearOptions(count int) {
	for i := 0; i < count; i++ {
		_, _ = fmt.Fprintf(p.out, "\033[A%s\r", ansiClearLine)
	}
}

// confirm displays a yes/no prompt. Returns defaultYes on empty input or EOF.
func (p *prompter) confirm(question string, defaultYes bool) bool {
	hint := "[y/N]"
	if defaultYes {
		hint = "[Y/n]"
	}

	if p.isTTY {
		_, _ = fmt.Fprintf(p.out, "\n%s%s%s %s%s%s: ", ansiBold, question, ansiReset, ansiDim, hint, ansiReset)
	} else {
		_, _ = fmt.Fprintf(p.out, "\n%s %s: ", question, hint)
	}

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
