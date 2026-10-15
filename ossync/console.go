package main

import (
	"io"
	"os"

	"github.com/mattn/go-colorable"
	"github.com/mattn/go-isatty"
)

// consoleOutput provides ANSI color compatibility for Windows consoles and
// strips color escapes when output is redirected to a file or pipe.
type consoleOutput struct {
	io.Writer
	terminal bool
}

func newConsoleOutput(file *os.File) *consoleOutput {
	terminal := isatty.IsTerminal(file.Fd()) || isatty.IsCygwinTerminal(file.Fd())
	var writer io.Writer = colorable.NewNonColorable(file)
	if terminal {
		writer = colorable.NewColorable(file)
	}
	return &consoleOutput{Writer: writer, terminal: terminal}
}

func (o *consoleOutput) IsTerminal() bool {
	return o.terminal
}
