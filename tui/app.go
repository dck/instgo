package tui

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"instgo/ig"
)

var (
	accent    = lipgloss.Color("205")
	accentAlt = lipgloss.Color("39")
	muted     = lipgloss.Color("244")
	faint     = lipgloss.Color("238")
	danger    = lipgloss.Color("203")
	ok        = lipgloss.Color("42")

	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(accent)
	mutedStyle = lipgloss.NewStyle().Foreground(muted)
	errorStyle = lipgloss.NewStyle().Foreground(danger)
	okStyle    = lipgloss.NewStyle().Foreground(ok)
)

type screen int

const (
	screenLogin screen = iota
	screenChat
)

type loggedInMsg struct{}

type sessionExpiredMsg struct{}

type App struct {
	client *ig.Client
	screen screen
	login  loginModel
	chat   chatModel
	width  int
	height int
}

func New(client *ig.Client) App {
	a := App{client: client, login: newLogin(client, "")}
	if client.LoggedIn() {
		a.screen = screenChat
		a.chat = newChat(client)
	}
	return a
}

func (a App) Init() tea.Cmd {
	if a.screen == screenChat {
		return a.chat.Init()
	}
	return a.login.Init()
}

func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width, a.height = msg.Width, msg.Height
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			return a, tea.Quit
		}
	case loggedInMsg:
		a.screen = screenChat
		a.chat = newChat(a.client)
		a.chat.resize(a.width, a.height)
		return a, a.chat.Init()
	case sessionExpiredMsg:
		_ = a.client.ClearAuth()
		a.screen = screenLogin
		a.login = newLogin(a.client, "Session expired, please log in again.")
		a.login.resize(a.width, a.height)
		return a, a.login.Init()
	}
	var cmd tea.Cmd
	if a.screen == screenChat {
		a.chat, cmd = a.chat.Update(msg)
	} else {
		a.login, cmd = a.login.Update(msg)
	}
	return a, cmd
}

func (a App) View() tea.View {
	var v tea.View
	if a.screen == screenChat {
		v = tea.NewView(a.chat.View())
		v.MouseMode = tea.MouseModeCellMotion
	} else {
		v = tea.NewView(a.login.View())
	}
	v.AltScreen = true
	v.WindowTitle = "instgo"
	return v
}
