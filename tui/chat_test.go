package tui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/dck/instgo/ig"
)

func ids(items []ig.Item) string {
	s := ""
	for _, it := range items {
		s += it.ItemID
	}
	return s
}

func TestMergeItemsLatestDropsUnsentKeepsNewer(t *testing.T) {
	existing := []ig.Item{{ItemID: "a", Timestamp: 1}, {ItemID: "b", Timestamp: 2}, {ItemID: "x", Timestamp: 3}, {ItemID: "c", Timestamp: 4}, {ItemID: "s", Timestamp: 9}}
	page := []ig.Item{{ItemID: "b", Timestamp: 2}, {ItemID: "c", Timestamp: 4}, {ItemID: "d", Timestamp: 5}}
	if got := ids(mergeItems(existing, page, true)); got != "abcds" {
		t.Fatalf("got %s", got)
	}
}

func TestMergeItemsOlderPrepends(t *testing.T) {
	existing := []ig.Item{{ItemID: "c", Timestamp: 3}}
	page := []ig.Item{{ItemID: "a", Timestamp: 1}, {ItemID: "b", Timestamp: 2}}
	if got := ids(mergeItems(existing, page, false)); got != "abc" {
		t.Fatalf("got %s", got)
	}
}

func TestViewFitsTinyWindows(t *testing.T) {
	m := newChat(ig.New(ig.NewSession(), ""))
	for h := 1; h <= 12; h++ {
		for _, w := range []int{1, 20, 80} {
			m.resize(w, h)
			_ = m.View()
		}
	}
}

func idleTestChat(t *testing.T) chatModel {
	return newChat(ig.New(ig.NewSession(), t.TempDir()+"/session.json"))
}

func TestIdleHidesTextAfterTimeout(t *testing.T) {
	m := idleTestChat(t)
	m.lastInput = time.Now().Add(-idleHide - time.Second)
	m, _ = m.Update(idleMsg{})
	if !m.disguised {
		t.Fatal("text still shown after idle timeout")
	}
}

func TestIdleKeepsTextWhileActive(t *testing.T) {
	m := idleTestChat(t)
	m, _ = m.Update(tea.KeyPressMsg{Code: 'j'})
	m, _ = m.Update(idleMsg{})
	if m.disguised {
		t.Fatal("text hidden right after a key press")
	}
}

func TestBlurHidesText(t *testing.T) {
	m := idleTestChat(t)
	m, _ = m.Update(tea.BlurMsg{})
	if !m.disguised {
		t.Fatal("text still shown after losing focus")
	}
}

func TestUnavailable(t *testing.T) {
	cases := []struct {
		name   string
		thread ig.Thread
		want   bool
	}{
		{"active user", ig.Thread{Users: []ig.User{{PK: "2", Username: "bob"}}}, false},
		{"no username", ig.Thread{Users: []ig.User{{PK: "2", FullName: "Instagram User"}}}, true},
		{"no users", ig.Thread{}, true},
		{"only me", ig.Thread{Users: []ig.User{{PK: "1", Username: "me"}}}, true},
		{"group", ig.Thread{IsGroup: true}, false},
	}
	for _, c := range cases {
		if got := unavailable(c.thread, "1"); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestUnavailableChatCannotBeWritten(t *testing.T) {
	m := idleTestChat(t)
	m.me = "1"
	tv := &threadView{thread: ig.Thread{ThreadID: "t", Users: []ig.User{{PK: "2"}}}}
	m.threads = []*threadView{tv}
	m.byID["t"] = tv
	m.openID = "t"
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.focus == focusInput {
		t.Fatal("message box focused for an unavailable chat")
	}
	m.input.SetValue("hi")
	m.submit()
	if len(m.queue) != 0 {
		t.Fatal("message queued for an unavailable chat")
	}
}
