package tui

import (
	"hash/fnv"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
)

const idleHide = 20 * time.Second

type idleMsg struct{}

var decoyWords = strings.Fields(`
	update function config module handler request response client server cache
	index buffer stream token parser schema field value record branch commit
	merge build test check lint format deploy release version package import
	export return error result option default context session window layout
	render state event signal queue worker thread process memory storage file
	path folder source target output input format filter sort search match
	pattern rule policy layer service endpoint route header payload message
	the a an of to in on for with from by into over after before then also
	now this that these those it we should will can may might need want
	add remove rename move replace keep use read write run start stop open
	close create delete change fix improve refactor simplify extract inline
	clean simple small large quick careful safe stable correct missing
	existing current previous next first last same other each every both
	because so but and or if when while until unless since though instead
	looks seems works fails passes returns handles calls sets gets builds
	everything nothing something here there already still just only again
`)

func (m *chatModel) mask(s string) string {
	if !m.disguised {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = scramble(line)
	}
	return strings.Join(lines, "\n")
}

func scramble(s string) string {
	var out []string
	sentenceStart := true
	for _, f := range strings.Fields(s) {
		core := strings.ToLower(strings.TrimFunc(f, unicode.IsPunct))
		if core == "" {
			out = append(out, f)
			continue
		}
		tail := f[len(strings.TrimRight(f, ".,!?:")):]
		h := fnv.New32a()
		h.Write([]byte(core))
		w := decoyWords[h.Sum32()%uint32(len(decoyWords))]
		if sentenceStart {
			w = strings.ToUpper(w[:1]) + w[1:]
		}
		out = append(out, w+tail)
		sentenceStart = strings.ContainsAny(tail, ".!?")
	}
	return strings.Join(out, " ")
}

func (m *chatModel) setDisguised(on bool) {
	if m.disguised == on {
		return
	}
	m.disguised = on
	m.input.Placeholder = m.mask("Message…")
	m.refreshView(false)
}

func idleCheck(after time.Duration) tea.Cmd {
	return tea.Tick(after, func(time.Time) tea.Msg { return idleMsg{} })
}

func (m *chatModel) onIdle() tea.Cmd {
	if left := idleHide - time.Since(m.lastInput); left > 0 {
		return idleCheck(left)
	}
	m.setDisguised(true)
	return idleCheck(idleHide)
}
