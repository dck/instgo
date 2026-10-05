package main

import (
	"fmt"
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"github.com/dck/instgo/ig"
	"github.com/dck/instgo/tui"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "instgo:", err)
		os.Exit(1)
	}
}

func run() error {
	path, err := ig.DefaultPath()
	if err != nil {
		return err
	}
	logFile, err := openDebugLog(filepath.Join(filepath.Dir(path), "debug.log"))
	if err != nil {
		return err
	}
	defer func() { _ = logFile.Close() }()
	ig.SetDebugOutput(logFile)
	session, err := ig.LoadSession(path)
	if err != nil {
		return err
	}
	ig.Debugf("start: session=%s logged_in=%t user=%s", path, session.LoggedIn(), session.Username)
	client := ig.New(session, path)
	if _, err := tea.NewProgram(tui.New(client)).Run(); err != nil {
		return err
	}
	if client.LoggedIn() {
		return client.Save()
	}
	return nil
}

func openDebugLog(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if info, err := os.Stat(path); err == nil && info.Size() > 5<<20 {
		_ = os.Rename(path, path+".old")
	}
	return os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
}
