package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/mattn/go-isatty"
)

// relaunchedEnv marks a process that already went through ensureTerminal, so
// it never tries to relaunch itself a second time.
const relaunchedEnv = "MIGRATE_RELAUNCHED"

// terminalEmulators are tried in order; the flag is how each one is told
// "run this command and keep the window open for it".
var terminalEmulators = []struct {
	name string
	flag string
}{
	{"x-terminal-emulator", "-e"},
	{"gnome-terminal", "--"},
	{"konsole", "-e"},
	{"xfce4-terminal", "-x"},
	{"xterm", "-e"},
}

// ensureTerminal makes sure log output is visible to the user. On Windows,
// double-clicking a console-subsystem .exe already opens a console, so
// there's nothing to do. On Linux, double-clicking from a file manager
// launches the process with no attached terminal at all, so any log.Printf
// output (including commit progress) silently vanishes; this relaunches the
// binary inside a terminal emulator so the user can see it. Returns true if
// it relaunched (the caller should exit without doing anything else).
func ensureTerminal() bool {
	if os.Getenv(relaunchedEnv) != "" {
		return false
	}
	if isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd()) {
		return false
	}

	self, err := os.Executable()
	if err != nil {
		return false
	}

	for _, te := range terminalEmulators {
		path, err := exec.LookPath(te.name)
		if err != nil {
			continue
		}
		args := append([]string{te.flag, self}, os.Args[1:]...)
		cmd := exec.Command(path, args...)
		cmd.Env = append(os.Environ(), relaunchedEnv+"=1")
		if err := cmd.Start(); err != nil {
			continue
		}
		return true
	}

	fmt.Fprintln(os.Stderr, "no terminal emulator found; logs will not be visible")
	return false
}
