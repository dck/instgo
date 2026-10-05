package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/dck/instgo/ig"
)

func TestLoginAcceptsTyping(t *testing.T) {
	var model tea.Model = New(ig.New(ig.NewSession(), t.TempDir()+"/s.json"))
	model.Init()
	for _, r := range "tester" {
		model, _ = model.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if got := model.(App).login.user.Value(); got != "tester" {
		t.Fatalf("username = %q", got)
	}
}
