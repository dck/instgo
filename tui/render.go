package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"instgo/ig"
)

const groupGap = 5 * time.Minute

var (
	meStyle     = lipgloss.NewStyle().Bold(true).Foreground(accentAlt)
	themStyle   = lipgloss.NewStyle().Bold(true).Foreground(accent)
	unreadStyle = lipgloss.NewStyle().Foreground(accent)
	faintStyle  = lipgloss.NewStyle().Foreground(faint)
)

func (m *chatModel) resize(w, h int) {
	m.width, m.height = w, h
	rw := m.chatWidth()
	m.vp.SetWidth(rw)
	m.vp.SetHeight(max(1, h-1-2-4))
	m.input.SetWidth(max(1, rw-3))
	m.filter.SetWidth(max(1, m.listWidth()-6))
	m.scrollList()
	m.refreshView(false)
}

func (m *chatModel) listWidth() int {
	if m.width < 72 {
		return max(18, m.width/3)
	}
	return min(42, max(28, m.width*3/10))
}

func (m *chatModel) chatWidth() int {
	return max(10, m.width-m.listWidth()-2)
}

func (m *chatModel) listHeaderLines() int {
	if m.filtering || m.filter.Value() != "" {
		return 2
	}
	return 1
}

func (m *chatModel) listRows() int {
	return max(1, (m.height-1-2-m.listHeaderLines())/2)
}

func (m *chatModel) refreshView(bottom bool) {
	tv := m.open()
	if tv == nil || m.width == 0 {
		m.vp.SetContent("")
		return
	}
	stick := bottom || m.vp.AtBottom() || m.vp.TotalLineCount() == 0
	m.vp.SetContent(m.renderMessages(tv, m.chatWidth()))
	if stick {
		m.vp.GotoBottom()
	}
}

func (m chatModel) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}
	bodyH := m.height - 1
	body := lipgloss.JoinHorizontal(lipgloss.Top, m.renderList(bodyH), m.renderChat(bodyH))
	return lipgloss.JoinVertical(lipgloss.Left, body, m.renderStatus())
}

func pane(focused bool, w, h int) lipgloss.Style {
	color := faint
	if focused {
		color = accent
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(color).
		Width(w).
		Height(h).
		MaxHeight(h)
}

func (m *chatModel) renderList(h int) string {
	w := m.listWidth()
	inner, innerH := w-2, h-2
	header := titleStyle.Render("Chats")
	if m.inbox.loading {
		header += " " + m.spinner.View()
	}
	lines := []string{header}
	if m.filtering || m.filter.Value() != "" {
		lines = append(lines, m.filter.View())
	}
	vis := m.visible()
	switch {
	case len(vis) == 0 && m.inbox.loading:
		lines = append(lines, mutedStyle.Render("Loading…"))
	case len(vis) == 0:
		lines = append(lines, mutedStyle.Render("No conversations"))
	}
	end := min(len(vis), m.listTop+m.listRows())
	for i := m.listTop; i < end; i++ {
		lines = append(lines, m.renderRow(vis[i], i == m.selected, inner)...)
	}
	if m.inbox.loadingOlder && len(lines) < innerH {
		lines = append(lines, mutedStyle.Render(" "+m.spinner.View()+" loading more…"))
	}
	return pane(m.focus == focusList || m.filtering, w, h).Render(fit(lines, innerH))
}

func (m *chatModel) renderRow(tv *threadView, selected bool, width int) []string {
	bar := " "
	nameStyle := lipgloss.NewStyle()
	if selected {
		bar = faintStyle.Render("▌")
		if m.focus == focusList || m.filtering {
			bar = unreadStyle.Render("▌")
		}
		nameStyle = nameStyle.Foreground(accent)
	}
	dot := "  "
	if tv.unread(m.me) {
		dot = unreadStyle.Render("● ")
		nameStyle = nameStyle.Bold(true)
	}
	when := ""
	if len(tv.items) > 0 {
		when = shortTime(tv.items[len(tv.items)-1].Timestamp.Time())
	} else if tv.thread.LastActivityAt > 0 {
		when = shortTime(tv.thread.LastActivityAt.Time())
	}
	nameW := max(1, width-3-lipgloss.Width(when)-1)
	name := nameStyle.Render(ansi.Truncate(threadName(tv.thread, m.me), nameW, "…"))
	gap := max(1, width-1-2-lipgloss.Width(name)-lipgloss.Width(when))
	line1 := bar + dot + name + strings.Repeat(" ", gap) + mutedStyle.Render(when)

	preview := ""
	if len(tv.items) > 0 {
		last := tv.items[len(tv.items)-1]
		preview = oneLine(itemText(last))
		if string(last.UserID) == m.me {
			preview = "You: " + preview
		}
	}
	if n := len(tv.pending); n > 0 {
		preview = "You: " + oneLine(tv.pending[n-1].text)
	}
	line2 := bar + "  " + mutedStyle.Render(ansi.Truncate(preview, max(1, width-3), "…"))
	return []string{line1, line2}
}

func (m *chatModel) renderChat(h int) string {
	w := m.chatWidth()
	inner, innerH := w, h-2
	focused := m.focus == focusInput && !m.filtering
	tv := m.open()
	if tv == nil {
		msg := mutedStyle.Render("Select a conversation")
		return pane(focused, w+2, h).Render(lipgloss.Place(inner, innerH, lipgloss.Center, lipgloss.Center, msg))
	}
	title := lipgloss.NewStyle().Bold(true).Render(threadName(tv.thread, m.me))
	if handles := usernames(tv.thread); handles != "" {
		title += mutedStyle.Render("  " + handles)
	}
	if tv.loading || tv.loadingOlder {
		title += " " + m.spinner.View()
	}
	divider := faintStyle.Render(strings.Repeat("─", inner))
	input := m.input.View()
	if !focused {
		input = mutedStyle.Render("› press enter to write a message")
	}
	lines := []string{ansi.Truncate(title, inner, "…"), divider, m.vp.View(), divider, input}
	return pane(focused, w+2, h).Render(fit(strings.Split(strings.Join(lines, "\n"), "\n"), innerH))
}

func (m *chatModel) renderStatus() string {
	left := okStyle.Render("●") + " @" + m.client.Username()
	switch {
	case m.status != "" && !m.statusOK:
		left += "  " + errorStyle.Render(oneLine(m.status))
	case len(m.queue) > 0:
		left += "  " + mutedStyle.Render(fmt.Sprintf("%s sending %d…", m.spinner.View(), len(m.queue)))
	case !m.synced.IsZero():
		left += "  " + mutedStyle.Render("synced "+m.synced.Format("15:04:05"))
	}
	var hints string
	switch {
	case m.filtering:
		hints = "type to filter · enter open · esc clear"
	case m.focus == focusInput:
		hints = "enter send · esc back · ↑↓/pgup/pgdn scroll · ctrl+r retry"
	default:
		hints = "j/k move · enter chat · / filter · r refresh · q quit"
	}
	hints = mutedStyle.Render(hints)
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(hints)
	if gap < 1 {
		return ansi.Truncate(left, m.width, "…")
	}
	return left + strings.Repeat(" ", gap) + hints
}

func (m *chatModel) renderMessages(tv *threadView, width int) string {
	names := map[string]string{}
	for _, u := range tv.thread.Users {
		names[string(u.PK)] = orDefault(u.FullName, u.Username)
	}
	body := lipgloss.NewStyle().Width(width).PaddingLeft(2)
	center := func(s string) string { return lipgloss.PlaceHorizontal(width, lipgloss.Center, s) }

	var out []string
	switch {
	case tv.loadingOlder:
		out = append(out, center(mutedStyle.Render("loading older messages…")))
	case tv.loaded && !tv.hasOlder:
		out = append(out, center(faintStyle.Render("beginning of conversation")))
	}
	var lastSender, lastDay string
	var lastAt time.Time
	header := func(sender string, at time.Time) {
		day := at.Format("2006-01-02")
		if day != lastDay {
			out = append(out, "", center(faintStyle.Render("── "+dayLabel(at)+" ──")))
			lastDay, lastSender = day, ""
		}
		if sender != lastSender || at.Sub(lastAt) > groupGap {
			name := themStyle.Render(orDefault(names[sender], "Unknown"))
			if sender == m.me {
				name = meStyle.Render("You")
			}
			out = append(out, "", name+"  "+mutedStyle.Render(at.Format("15:04")))
		}
		lastSender, lastAt = sender, at
	}
	for _, it := range tv.items {
		at := it.Timestamp.Time().Local()
		if it.ItemType == "action_log" {
			header("", at)
			out = append(out, center(mutedStyle.Italic(true).Render(itemText(it))))
			lastSender = ""
			continue
		}
		header(string(it.UserID), at)
		if it.RepliedTo != nil {
			quote := "↪ " + oneLine(itemText(*it.RepliedTo))
			out = append(out, body.Render(faintStyle.Render(ansi.Truncate(quote, max(1, width-2), "…"))))
		}
		out = append(out, body.Render(itemText(it)))
	}
	for _, p := range tv.pending {
		header(m.me, p.at)
		state := mutedStyle.Render("  sending…")
		if p.failed {
			state = errorStyle.Render("  ✗ not sent · ctrl+r to retry")
		}
		out = append(out, body.Render(p.text+state))
	}
	return strings.Join(out, "\n")
}

func itemText(it ig.Item) string {
	switch it.ItemType {
	case "text":
		return it.Text
	case "link":
		if it.Link != nil && it.Link.Text != "" {
			return it.Link.Text
		}
	case "like":
		return orDefault(it.Like, "❤️")
	case "media":
		if it.Media != nil && it.Media.MediaType == 2 {
			return "[video]"
		}
		return "[photo]"
	case "raven_media", "visual_media":
		return "[disappearing media]"
	case "voice_media":
		return "[voice message]"
	case "animated_media":
		return "[GIF]"
	case "media_share", "xma_media_share", "clip", "xma_clip", "felix_share":
		return "[shared a post]"
	case "story_share", "xma_story_share":
		return "[shared a story]"
	case "reel_share":
		if it.ReelShare != nil && it.ReelShare.Text != "" {
			return "[story reply] " + it.ReelShare.Text
		}
		return "[story reply]"
	case "placeholder":
		if it.Placeholder != nil {
			return "[" + it.Placeholder.Message + "]"
		}
	case "action_log":
		if it.ActionLog != nil {
			return it.ActionLog.Description
		}
	}
	if it.Text != "" {
		return it.Text
	}
	return "[" + strings.ReplaceAll(it.ItemType, "_", " ") + "]"
}

func threadName(t ig.Thread, me string) string {
	if t.IsGroup && t.Title != "" {
		return t.Title
	}
	var names []string
	for _, u := range t.Users {
		if string(u.PK) != me {
			names = append(names, orDefault(u.FullName, u.Username))
		}
	}
	if len(names) == 0 {
		return orDefault(t.Title, "Unknown")
	}
	return strings.Join(names, ", ")
}

func usernames(t ig.Thread) string {
	var out []string
	for _, u := range t.Users {
		if u.Username != "" {
			out = append(out, "@"+u.Username)
		}
	}
	return strings.Join(out, ", ")
}

func shortTime(t time.Time) string {
	now := time.Now()
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case sameDay(t, now):
		return t.Local().Format("15:04")
	case d < 7*24*time.Hour:
		return t.Local().Format("Mon")
	case t.Year() == now.Year():
		return t.Local().Format("2 Jan")
	}
	return t.Local().Format("Jan 2006")
}

func dayLabel(t time.Time) string {
	now := time.Now()
	switch {
	case sameDay(t, now):
		return "Today"
	case sameDay(t, now.AddDate(0, 0, -1)):
		return "Yesterday"
	case now.Sub(t) < 7*24*time.Hour:
		return t.Format("Monday")
	case t.Year() == now.Year():
		return t.Format("Mon, 2 Jan")
	}
	return t.Format("Mon, 2 Jan 2006")
}

func sameDay(a, b time.Time) bool {
	a, b = a.Local(), b.Local()
	return a.Year() == b.Year() && a.YearDay() == b.YearDay()
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func fit(lines []string, h int) string {
	if len(lines) > h {
		lines = lines[:h]
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}
