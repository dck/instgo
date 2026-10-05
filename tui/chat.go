package tui

import (
	"context"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/dck/instgo/ig"
)

const (
	pollEvery      = 5 * time.Second
	inboxEveryTick = 6
	previewDelay   = 250 * time.Millisecond
	requestTimeout = 45 * time.Second
)

type focusArea int

const (
	focusList focusArea = iota
	focusInput
)

type pending struct {
	clientContext string
	threadID      string
	text          string
	at            time.Time
	failed        bool
}

type threadView struct {
	thread       ig.Thread
	items        []ig.Item
	loaded       bool
	loading      bool
	loadingOlder bool
	cursor       string
	hasOlder     bool
	pending      []*pending
	seenItem     string
}

type inboxMsg struct {
	inbox *ig.Inbox
	older bool
	err   error
}

type threadMsg struct {
	id     string
	thread *ig.Thread
	older  bool
	err    error
}

type sentMsg struct {
	p    *pending
	item *ig.Item
	err  error
}

type seenMsg struct{ err error }

type tickMsg struct{}

type previewMsg struct{ id string }

type chatModel struct {
	client *ig.Client
	me     string

	threads   []*threadView
	byID      map[string]*threadView
	selected  int
	listTop   int
	openID    string
	focus     focusArea
	filtering bool
	filter    textinput.Model
	disguised bool

	input   textinput.Model
	vp      viewport.Model
	spinner spinner.Model
	queue   []*pending
	sending bool
	ticks   int
	inbox   struct {
		loading, loadingOlder, hasOlder bool
		cursor                          string
	}
	status   string
	statusOK bool
	synced   time.Time

	width, height int
}

func newChat(client *ig.Client) chatModel {
	input := textinput.New()
	input.Prompt = "› "
	input.Placeholder = "Message…"
	input.CharLimit = 1000

	filter := textinput.New()
	filter.Prompt = "/ "
	filter.Placeholder = "filter"

	vp := viewport.New()
	vp.MouseWheelDelta = 3

	m := chatModel{
		client:  client,
		me:      client.UserID(),
		byID:    map[string]*threadView{},
		input:   input,
		filter:  filter,
		vp:      vp,
		spinner: spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(lipgloss.NewStyle().Foreground(accent))),
	}
	m.inbox.loading = true
	return m
}

func (m chatModel) Init() tea.Cmd {
	return tea.Batch(m.fetchInbox(""), m.spinner.Tick, tick())
}

func tick() tea.Cmd {
	return tea.Tick(pollEvery, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m chatModel) fetchInbox(cursor string) tea.Cmd {
	client := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		inbox, err := client.Inbox(ctx, cursor)
		return inboxMsg{inbox: inbox, older: cursor != "", err: err}
	}
}

func (m chatModel) fetchThread(id, cursor string) tea.Cmd {
	client := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		t, err := client.Thread(ctx, id, cursor)
		return threadMsg{id: id, thread: t, older: cursor != "", err: err}
	}
}

func (m chatModel) sendCmd(p *pending) tea.Cmd {
	client := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		item, err := client.SendText(ctx, p.threadID, p.text, p.clientContext)
		return sentMsg{p: p, item: item, err: err}
	}
}

func (m chatModel) markSeenCmd(threadID, itemID string) tea.Cmd {
	client := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		return seenMsg{client.MarkSeen(ctx, threadID, itemID)}
	}
}

func (m *chatModel) fail(err error) tea.Cmd {
	if ig.IsLoginRequired(err) {
		return func() tea.Msg { return sessionExpiredMsg{} }
	}
	m.status, m.statusOK = err.Error(), false
	return nil
}

func (m chatModel) Update(msg tea.Msg) (chatModel, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.resize(msg.Width, msg.Height)
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case tickMsg:
		m.ticks++
		cmds = append(cmds, tick())
		if tv := m.open(); tv != nil && tv.loaded && !tv.loading {
			tv.loading = true
			cmds = append(cmds, m.fetchThread(tv.thread.ThreadID, ""))
		}
		if m.ticks%inboxEveryTick == 0 && !m.inbox.loading {
			m.inbox.loading = true
			cmds = append(cmds, m.fetchInbox(""))
		}
	case previewMsg:
		if tv := m.open(); tv != nil && tv.thread.ThreadID == msg.id && !tv.loaded && !tv.loading {
			tv.loading = true
			cmds = append(cmds, m.fetchThread(msg.id, ""))
		}
	case inboxMsg:
		cmds = append(cmds, m.onInbox(msg))
	case threadMsg:
		cmds = append(cmds, m.onThread(msg))
	case sentMsg:
		cmds = append(cmds, m.onSent(msg))
	case seenMsg:
	case tea.KeyPressMsg:
		cmds = append(cmds, m.onKey(msg))
	case tea.MouseClickMsg:
		cmds = append(cmds, m.onClick(msg.Mouse()))
	case tea.MouseWheelMsg:
		mouse := msg.Mouse()
		if mouse.X < m.listWidth() {
			switch mouse.Button {
			case tea.MouseWheelUp:
				cmds = append(cmds, m.moveSelection(-1))
			case tea.MouseWheelDown:
				cmds = append(cmds, m.moveSelection(1))
			}
		} else {
			var cmd tea.Cmd
			m.vp, cmd = m.vp.Update(msg)
			cmds = append(cmds, cmd, m.maybeLoadOlder())
		}
	default:
		if m.focus == focusInput {
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			cmds = append(cmds, cmd)
		}
	}
	return m, tea.Batch(cmds...)
}

func (m *chatModel) onInbox(msg inboxMsg) tea.Cmd {
	if msg.older {
		m.inbox.loadingOlder = false
	} else {
		m.inbox.loading = false
	}
	if msg.err != nil {
		return m.fail(msg.err)
	}
	selectedID := m.selectedID()
	fresh := make([]*threadView, 0, len(msg.inbox.Threads))
	seen := map[string]bool{}
	for _, t := range msg.inbox.Threads {
		tv := m.byID[t.ThreadID]
		if tv == nil {
			tv = &threadView{}
			m.byID[t.ThreadID] = tv
		} else if msg.older {
			continue
		}
		tv.thread = t
		if !tv.loaded {
			tv.items = reversed(t.Items)
		} else {
			tv.items = mergeItems(tv.items, reversed(t.Items), false)
		}
		fresh = append(fresh, tv)
		seen[t.ThreadID] = true
	}
	var rest []*threadView
	for _, tv := range m.threads {
		if !seen[tv.thread.ThreadID] {
			rest = append(rest, tv)
		}
	}
	if msg.older {
		m.threads = append(m.threads, fresh...)
	} else {
		m.threads = append(fresh, rest...)
	}
	if msg.older || m.inbox.cursor == "" {
		m.inbox.cursor = msg.inbox.OldestCursor
		m.inbox.hasOlder = msg.inbox.HasOlder
	}
	m.synced = time.Now()
	if !m.statusOK && m.status != "" {
		m.status = ""
	}
	m.reselect(selectedID)
	if m.openID == "" && len(m.visible()) > 0 {
		return m.openSelected()
	}
	m.refreshView(false)
	return nil
}

func (m *chatModel) onThread(msg threadMsg) tea.Cmd {
	tv := m.byID[msg.id]
	if tv == nil {
		return nil
	}
	if msg.older {
		tv.loadingOlder = false
	} else {
		tv.loading = false
	}
	if msg.err != nil {
		return m.fail(msg.err)
	}
	page := reversed(msg.thread.Items)
	if msg.older || !tv.loaded {
		tv.cursor = msg.thread.OldestCursor
		tv.hasOlder = msg.thread.HasOlder
	}
	tv.items = mergeItems(tv.items, page, !msg.older)
	tv.loaded = true
	if len(msg.thread.Users) > 0 {
		msg.thread.Items = nil
		tv.thread = *msg.thread
	}
	tv.reconcile()
	if msg.id != m.openID {
		return nil
	}
	before := m.vp.TotalLineCount()
	offset := m.vp.YOffset()
	m.refreshView(false)
	if msg.older {
		m.vp.SetYOffset(offset + m.vp.TotalLineCount() - before)
	}
	return m.markSeen(tv)
}

func (m *chatModel) onSent(msg sentMsg) tea.Cmd {
	m.sending = false
	if len(m.queue) > 0 && m.queue[0] == msg.p {
		m.queue = m.queue[1:]
	}
	tv := m.byID[msg.p.threadID]
	if msg.err != nil {
		msg.p.failed = true
		cmd := m.fail(msg.err)
		m.refreshView(false)
		return tea.Batch(cmd, m.sendNext())
	}
	if tv != nil {
		tv.items = mergeItems(tv.items, []ig.Item{*msg.item}, false)
		tv.reconcile()
		tv.thread.LastActivityAt = msg.item.Timestamp
		m.bumpThread(tv)
	}
	m.refreshView(true)
	return m.sendNext()
}

func (m *chatModel) sendNext() tea.Cmd {
	if m.sending || len(m.queue) == 0 {
		return nil
	}
	m.sending = true
	return m.sendCmd(m.queue[0])
}

func (m *chatModel) onKey(msg tea.KeyPressMsg) tea.Cmd {
	key := msg.String()
	if key == "ctrl+x" {
		m.toggleDisguise()
		return nil
	}
	if m.filtering {
		switch key {
		case "esc":
			m.filtering = false
			m.filter.Reset()
			m.filter.Blur()
			m.reselect(m.openID)
			return nil
		case "enter", "down", "up":
			m.filtering = false
			m.filter.Blur()
			if key == "enter" {
				return m.openSelected()
			}
			return nil
		}
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(msg)
		m.selected, m.listTop = 0, 0
		return tea.Batch(cmd, m.openSelected())
	}
	if m.focus == focusInput {
		switch key {
		case "esc", "tab", "shift+tab":
			m.focus = focusList
			m.input.Blur()
			return nil
		case "enter":
			return m.submit()
		case "ctrl+r":
			return m.retryFailed()
		case "up":
			m.vp.ScrollUp(1)
			return m.maybeLoadOlder()
		case "down":
			m.vp.ScrollDown(1)
			return nil
		case "pgup", "ctrl+u":
			m.vp.HalfPageUp()
			return m.maybeLoadOlder()
		case "pgdown", "ctrl+d":
			m.vp.HalfPageDown()
			return nil
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return cmd
	}
	switch key {
	case "q":
		return tea.Quit
	case "j", "down":
		return m.moveSelection(1)
	case "k", "up":
		return m.moveSelection(-1)
	case "g", "home":
		return m.moveSelection(-len(m.threads))
	case "G", "end":
		return m.moveSelection(len(m.threads))
	case "/":
		m.filtering = true
		return m.filter.Focus()
	case "esc":
		if m.filter.Value() != "" {
			m.filter.Reset()
			m.reselect(m.openID)
		}
		return nil
	case "r":
		if m.inbox.loading {
			return nil
		}
		m.inbox.loading = true
		return m.fetchInbox("")
	case "pgup", "ctrl+u":
		m.vp.HalfPageUp()
		return m.maybeLoadOlder()
	case "pgdown", "ctrl+d":
		m.vp.HalfPageDown()
		return nil
	case "enter", "l", "right", "tab", "i":
		if m.open() == nil {
			return nil
		}
		m.focus = focusInput
		return tea.Batch(m.input.Focus(), m.markSeen(m.open()))
	}
	return nil
}

func (m *chatModel) onClick(mouse tea.Mouse) tea.Cmd {
	if mouse.X >= m.listWidth() {
		if m.open() == nil {
			return nil
		}
		m.focus = focusInput
		return tea.Batch(m.input.Focus(), m.markSeen(m.open()))
	}
	row := mouse.Y - 1 - m.listHeaderLines()
	if row < 0 {
		return nil
	}
	idx := m.listTop + row/2
	if idx >= len(m.visible()) {
		return nil
	}
	m.focus = focusList
	m.input.Blur()
	return m.moveSelection(idx - m.selected)
}

func (m *chatModel) submit() tea.Cmd {
	text := strings.TrimSpace(m.input.Value())
	tv := m.open()
	if text == "" || tv == nil {
		return nil
	}
	m.input.Reset()
	p := &pending{clientContext: ig.NewClientContext(), threadID: tv.thread.ThreadID, text: text, at: time.Now()}
	tv.pending = append(tv.pending, p)
	m.queue = append(m.queue, p)
	m.refreshView(true)
	return m.sendNext()
}

func (m *chatModel) retryFailed() tea.Cmd {
	tv := m.open()
	if tv == nil {
		return nil
	}
	for _, p := range tv.pending {
		if p.failed {
			p.failed = false
			m.queue = append(m.queue, p)
		}
	}
	m.refreshView(true)
	return m.sendNext()
}

func (m *chatModel) moveSelection(delta int) tea.Cmd {
	vis := m.visible()
	if len(vis) == 0 {
		return nil
	}
	m.selected = max(0, min(len(vis)-1, m.selected+delta))
	m.scrollList()
	var cmds []tea.Cmd
	if m.selected >= len(vis)-3 && m.inbox.hasOlder && !m.inbox.loadingOlder && m.filter.Value() == "" {
		m.inbox.loadingOlder = true
		cmds = append(cmds, m.fetchInbox(m.inbox.cursor))
	}
	return tea.Batch(append(cmds, m.openSelected())...)
}

func (m *chatModel) openSelected() tea.Cmd {
	vis := m.visible()
	if len(vis) == 0 {
		return nil
	}
	tv := vis[m.selected]
	if tv.thread.ThreadID == m.openID {
		return nil
	}
	m.openID = tv.thread.ThreadID
	m.refreshView(true)
	id := m.openID
	return tea.Tick(previewDelay, func(time.Time) tea.Msg { return previewMsg{id} })
}

func (m *chatModel) maybeLoadOlder() tea.Cmd {
	tv := m.open()
	if tv == nil || !m.vp.AtTop() || !tv.loaded || !tv.hasOlder || tv.loadingOlder || tv.cursor == "" {
		return nil
	}
	tv.loadingOlder = true
	return m.fetchThread(tv.thread.ThreadID, tv.cursor)
}

func (m *chatModel) markSeen(tv *threadView) tea.Cmd {
	if tv == nil || m.focus != focusInput || tv.thread.ThreadID != m.openID || len(tv.items) == 0 {
		return nil
	}
	last := tv.items[len(tv.items)-1]
	if string(last.UserID) == m.me || last.ItemID == tv.seenItem || !tv.unread(m.me) {
		return nil
	}
	tv.seenItem = last.ItemID
	if tv.thread.LastSeenAt == nil {
		tv.thread.LastSeenAt = map[string]ig.LastSeen{}
	}
	tv.thread.LastSeenAt[m.me] = ig.LastSeen{ItemID: last.ItemID, Timestamp: last.Timestamp}
	return m.markSeenCmd(tv.thread.ThreadID, last.ItemID)
}

func (m *chatModel) bumpThread(tv *threadView) {
	selectedID := m.selectedID()
	i := slices.Index(m.threads, tv)
	if i <= 0 {
		return
	}
	m.threads = slices.Insert(slices.Delete(m.threads, i, i+1), 0, tv)
	m.reselect(selectedID)
}

func (m *chatModel) open() *threadView {
	return m.byID[m.openID]
}

func (m *chatModel) selectedID() string {
	vis := m.visible()
	if m.selected < len(vis) {
		return vis[m.selected].thread.ThreadID
	}
	return ""
}

func (m *chatModel) reselect(id string) {
	for i, tv := range m.visible() {
		if tv.thread.ThreadID == id {
			m.selected = i
			m.scrollList()
			return
		}
	}
	m.selected = max(0, min(m.selected, len(m.visible())-1))
	m.scrollList()
}

func (m *chatModel) visible() []*threadView {
	q := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	if q == "" {
		return m.threads
	}
	var out []*threadView
	for _, tv := range m.threads {
		if fuzzy(strings.ToLower(threadName(tv.thread, m.me)+" "+usernames(tv.thread)), q) {
			out = append(out, tv)
		}
	}
	return out
}

func (m *chatModel) scrollList() {
	rows := m.listRows()
	if rows <= 0 {
		return
	}
	if m.selected < m.listTop {
		m.listTop = m.selected
	}
	if m.selected >= m.listTop+rows {
		m.listTop = m.selected - rows + 1
	}
}

func (tv *threadView) reconcile() {
	sent := map[string]bool{}
	for _, it := range tv.items {
		if it.ClientContext != "" {
			sent[it.ClientContext] = true
		}
	}
	tv.pending = slices.DeleteFunc(tv.pending, func(p *pending) bool { return sent[p.clientContext] })
}

func (tv *threadView) unread(me string) bool {
	var last *ig.Item
	if len(tv.items) > 0 {
		last = &tv.items[len(tv.items)-1]
	}
	if last == nil || string(last.UserID) == me {
		return false
	}
	seen, ok := tv.thread.LastSeenAt[me]
	if !ok {
		return true
	}
	return seen.ItemID != last.ItemID && seen.Timestamp < last.Timestamp
}

func mergeItems(existing, page []ig.Item, latest bool) []ig.Item {
	if len(page) == 0 {
		return existing
	}
	inPage := map[string]bool{}
	for _, it := range page {
		inPage[it.ItemID] = true
	}
	oldest, newest := page[0].Timestamp, page[len(page)-1].Timestamp
	out := make([]ig.Item, 0, len(existing)+len(page))
	for _, it := range existing {
		if inPage[it.ItemID] {
			continue
		}
		if latest && it.Timestamp >= oldest && it.Timestamp <= newest {
			continue
		}
		out = append(out, it)
	}
	out = append(out, page...)
	slices.SortStableFunc(out, func(a, b ig.Item) int {
		switch {
		case a.Timestamp < b.Timestamp:
			return -1
		case a.Timestamp > b.Timestamp:
			return 1
		}
		return 0
	})
	return out
}

func reversed(items []ig.Item) []ig.Item {
	out := slices.Clone(items)
	slices.Reverse(out)
	return out
}

func fuzzy(s, q string) bool {
	rq := []rune(q)
	i := 0
	for _, r := range s {
		if i < len(rq) && r == rq[i] {
			i++
		}
	}
	return i == len(rq)
}
