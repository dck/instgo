package main

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"

	"instgo/ig"
	"instgo/tui"
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
	session, err := ig.LoadSession(path)
	if err != nil {
		return err
	}
	client := ig.New(session, path)
	if _, err := tea.NewProgram(tui.New(client)).Run(); err != nil {
		return err
	}
	if client.LoggedIn() {
		return client.Save()
	}
	return nil
}
