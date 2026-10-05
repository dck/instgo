package tui

import (
	"context"
	"errors"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"instgo/ig"
)

const loginTimeout = 3 * time.Minute

type loginStep int

const (
	stepUsername loginStep = iota
	stepPassword
	stepCode
)

type loginResultMsg struct{ err error }

type challengeMsg struct {
	ch  *ig.Challenge
	err error
}

type loginModel struct {
	client  *ig.Client
	step    loginStep
	user    textinput.Model
	pass    textinput.Model
	code    textinput.Model
	spinner spinner.Model
	busy    string
	info    string
	err     string
	twoFA   *ig.TwoFactorRequired
	verify  *ig.Challenge
	width   int
	height  int
}

func newLogin(client *ig.Client, notice string) loginModel {
	user := textinput.New()
	user.Prompt = "Username  "
	user.Placeholder = "your.instagram.handle"
	user.SetValue(client.Username())
	user.SetWidth(32)

	pass := textinput.New()
	pass.Prompt = "Password  "
	pass.EchoMode = textinput.EchoPassword
	pass.SetWidth(32)

	code := textinput.New()
	code.Prompt = "Code      "
	code.Placeholder = "123456"
	code.CharLimit = 8
	code.SetWidth(32)

	return loginModel{
		client:  client,
		user:    user,
		pass:    pass,
		code:    code,
		spinner: spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(lipgloss.NewStyle().Foreground(accent))),
		err:     notice,
	}
}

func (m loginModel) Init() tea.Cmd {
	return m.focus()
}

func (m *loginModel) resize(w, h int) {
	m.width, m.height = w, h
}

func (m *loginModel) focus() tea.Cmd {
	m.user.Blur()
	m.pass.Blur()
	m.code.Blur()
	switch m.step {
	case stepPassword:
		return m.pass.Focus()
	case stepCode:
		return m.code.Focus()
	default:
		return m.user.Focus()
	}
}

func (m *loginModel) setStep(step loginStep) tea.Cmd {
	m.step = step
	return m.focus()
}

func (m *loginModel) startBusy(text string, cmd tea.Cmd) tea.Cmd {
	m.busy = text
	m.err = ""
	return tea.Batch(cmd, m.spinner.Tick)
}

func (m loginModel) Update(msg tea.Msg) (loginModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.resize(msg.Width, msg.Height)
		return m, nil
	case spinner.TickMsg:
		if m.busy == "" {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case loginResultMsg:
		return m.handleLoginResult(msg.err)
	case challengeMsg:
		m.busy = ""
		if msg.err != nil {
			m.err = msg.err.Error()
			m.pass.Reset()
			return m, m.setStep(stepPassword)
		}
		if msg.ch.Done {
			return m, m.startBusy("Verified, logging in…", m.resumeCmd())
		}
		m.verify = msg.ch
		m.twoFA = nil
		m.info = "Instagram sent a security code to " + orDefault(msg.ch.Destination, "your email") + "."
		m.code.Reset()
		return m, m.setStep(stepCode)
	case tea.KeyPressMsg:
		if m.busy != "" {
			return m, nil
		}
		switch msg.String() {
		case "esc":
			if m.step != stepUsername {
				m.err, m.info = "", ""
				m.twoFA, m.verify = nil, nil
				return m, m.setStep(m.step - 1)
			}
			return m, tea.Quit
		case "enter":
			return m.submit()
		}
	}
	var cmd tea.Cmd
	switch m.step {
	case stepUsername:
		m.user, cmd = m.user.Update(msg)
	case stepPassword:
		m.pass, cmd = m.pass.Update(msg)
	case stepCode:
		m.code, cmd = m.code.Update(msg)
	}
	return m, cmd
}

func (m loginModel) submit() (loginModel, tea.Cmd) {
	switch m.step {
	case stepUsername:
		if strings.TrimSpace(m.user.Value()) == "" {
			return m, nil
		}
		m.err = ""
		return m, m.setStep(stepPassword)
	case stepPassword:
		if m.pass.Value() == "" {
			return m, nil
		}
		user, pass := strings.TrimSpace(m.user.Value()), m.pass.Value()
		client := m.client
		return m, m.startBusy("Logging in…", func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), loginTimeout)
			defer cancel()
			return loginResultMsg{client.Login(ctx, user, pass)}
		})
	case stepCode:
		code := strings.TrimSpace(m.code.Value())
		if code == "" {
			return m, nil
		}
		client, twoFA, verify := m.client, m.twoFA, m.verify
		return m, m.startBusy("Checking code…", func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), loginTimeout)
			defer cancel()
			if twoFA != nil {
				return loginResultMsg{client.TwoFactorLogin(ctx, twoFA, code)}
			}
			return loginResultMsg{client.SubmitChallengeCode(ctx, verify, code)}
		})
	}
	return m, nil
}

func (m loginModel) resumeCmd() tea.Cmd {
	client := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), loginTimeout)
		defer cancel()
		return loginResultMsg{client.ResumeLogin(ctx)}
	}
}

func (m loginModel) handleLoginResult(err error) (loginModel, tea.Cmd) {
	m.busy = ""
	var twoFA *ig.TwoFactorRequired
	var challenge *ig.ChallengeRequired
	switch {
	case err == nil:
		return m, func() tea.Msg { return loggedInMsg{} }
	case errors.As(err, &twoFA):
		m.twoFA, m.verify = twoFA, nil
		if twoFA.TOTP {
			m.info = "Enter the code from your authenticator app."
		} else {
			m.info = "Enter the code sent by SMS to " + orDefault(twoFA.Phone, "your phone") + "."
		}
		m.code.Reset()
		return m, m.setStep(stepCode)
	case errors.As(err, &challenge):
		client := m.client
		return m, m.startBusy("Instagram wants to verify it's you…", func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), loginTimeout)
			defer cancel()
			ch, err := client.StartChallenge(ctx, challenge)
			return challengeMsg{ch, err}
		})
	case errors.Is(err, ig.ErrWrongCode) && m.step == stepCode:
		m.err = err.Error()
		m.code.Reset()
		return m, nil
	}
	m.err = err.Error()
	m.info = ""
	m.twoFA, m.verify = nil, nil
	m.pass.Reset()
	return m, m.setStep(stepPassword)
}

func (m loginModel) View() string {
	logo := titleStyle.Render("instgo") + mutedStyle.Render("  ·  Instagram DMs in your terminal")
	lines := []string{logo, ""}
	lines = append(lines, m.user.View())
	if m.step >= stepPassword {
		lines = append(lines, m.pass.View())
	}
	if m.step == stepCode {
		lines = append(lines, "", m.info, m.code.View())
	}
	lines = append(lines, "")
	switch {
	case m.busy != "":
		lines = append(lines, m.spinner.View()+" "+m.busy)
	case m.err != "":
		lines = append(lines, errorStyle.Render(m.err))
	default:
		lines = append(lines, "")
	}
	lines = append(lines, "", mutedStyle.Render("enter continue · esc back · ctrl+c quit"))
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accent).
		Padding(1, 3).
		Width(min(64, max(40, m.width-4))).
		Render(strings.Join(lines, "\n"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
