// Package tui is the Bubble Tea interactive interface. It calls the
// same app layer as the CLI; keys map to gather→confirm→execute.
package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"midden/internal/adapter"
	"midden/internal/adapter/claude"
	"midden/internal/app"
	"midden/internal/format"
)

type Svc interface {
	Projects() ([]adapter.Project, error)
	Sessions(pid string) ([]adapter.Session, error)
	Preview(pid, sid string, n int) ([]adapter.Message, error)
	Rename(pid, sid, title string) error
	GatherSessions(pid string, sids []string) (*app.Plan, error)
	GatherProject(pid string) (*app.Plan, error)
	GatherClear(pid string, r claude.ClearRules) (*app.Plan, error)
	Execute(p *app.Plan) (*app.Result, error)
	ReclaimEstimate() (int64, error)
}

type mode int

const (
	modeProjects mode = iota
	modeSessions
	modeConfirm
	modePreview
	modeRename
)

type (
	projectsMsg []adapter.Project
	sessionsMsg []adapter.Session
	reclaimMsg  int64
	previewMsg  []adapter.Message
	planMsg     *app.Plan
	executedMsg *app.Result
	renamedMsg  struct{}
	errMsg      struct{ err error }
)

type Model struct {
	svc      Svc
	mode     mode
	prevMode mode

	projects  []adapter.Project
	filter    string
	filtering bool
	pcursor   int

	proj     adapter.Project
	sessions []adapter.Session
	scursor  int
	selected map[string]bool

	plan    *app.Plan
	preview []adapter.Message
	input   []rune

	reclaim int64 // -1 = computing
	status  string
	err     error
	width   int
	height  int
}

func New(svc Svc) Model {
	return Model{svc: svc, selected: map[string]bool{}, reclaim: -1}
}

func Run(svc Svc) error {
	_, err := tea.NewProgram(New(svc), tea.WithAltScreen()).Run()
	return err
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.loadProjects, m.loadReclaim)
}

func (m Model) loadProjects() tea.Msg {
	ps, err := m.svc.Projects()
	if err != nil {
		return errMsg{err}
	}
	return projectsMsg(ps)
}

func (m Model) loadReclaim() tea.Msg {
	n, err := m.svc.ReclaimEstimate()
	if err != nil {
		return reclaimMsg(0)
	}
	return reclaimMsg(n)
}

func (m Model) filtered() []adapter.Project {
	if m.filter == "" {
		return m.projects
	}
	var out []adapter.Project
	for _, p := range m.projects {
		if strings.Contains(strings.ToLower(p.Name), strings.ToLower(m.filter)) {
			out = append(out, p)
		}
	}
	return out
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case projectsMsg:
		m.projects = msg
		if m.pcursor >= len(m.filtered()) {
			m.pcursor = 0
		}
		return m, nil
	case reclaimMsg:
		m.reclaim = int64(msg)
		return m, nil
	case errMsg:
		m.err = msg.err
		return m, nil
	case sessionsMsg:
		m.sessions = msg
		if m.scursor >= len(m.sessions) {
			m.scursor = 0
		}
		return m, nil
	case planMsg:
		m.plan = msg
		m.mode = modeConfirm
		return m, nil
	case previewMsg:
		m.preview = msg
		m.mode = modePreview
		return m, nil
	case executedMsg:
		if msg.EntryID != "" {
			m.status = fmt.Sprintf("moved %d item(s) (%s) to trash — 'midden trash restore %s' to undo",
				msg.Count, format.Size(msg.SizeBytes), msg.EntryID)
		}
		m.selected = map[string]bool{}
		m.mode = m.prevListMode()
		if m.mode == modeSessions {
			return m, tea.Batch(m.loadSessions, m.loadProjects, m.loadReclaim)
		}
		return m, tea.Batch(m.loadProjects, m.loadReclaim)
	case renamedMsg:
		m.mode = modeSessions
		return m, m.loadSessions
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			return m, tea.Quit
		}
		if m.err != nil {
			m.err = nil
			m.mode = m.prevListMode()
			return m, nil
		}
		switch m.mode {
		case modeProjects:
			return m.updateProjects(msg)
		case modeSessions:
			return m.updateSessions(msg)
		case modeConfirm:
			return m.updateConfirm(msg)
		case modePreview:
			m.mode = modeSessions
			return m, nil
		case modeRename:
			return m.updateRename(msg)
		}
	}
	return m, nil
}

func (m Model) updateProjects(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.filtering {
		switch msg.Type {
		case tea.KeyEnter, tea.KeyEsc:
			m.filtering = false
			if msg.Type == tea.KeyEsc {
				m.filter = ""
			}
		case tea.KeyBackspace:
			if r := []rune(m.filter); len(r) > 0 {
				m.filter = string(r[:len(r)-1])
			}
		case tea.KeyRunes:
			m.filter += string(msg.Runes)
		}
		m.pcursor = 0
		return m, nil
	}
	list := m.filtered()
	switch msg.String() {
	case "q":
		return m, tea.Quit
	case "up", "k":
		if m.pcursor > 0 {
			m.pcursor--
		}
	case "down", "j":
		if m.pcursor < len(list)-1 {
			m.pcursor++
		}
	case "/":
		m.filtering = true
		m.filter = ""
	case "enter":
		if len(list) > 0 {
			m.proj = list[m.pcursor]
			m.mode = modeSessions
			m.scursor = 0
			m.selected = map[string]bool{}
			m.sessions = nil
			return m, m.loadSessions
		}
	case "d":
		if len(list) > 0 {
			m.prevMode = modeProjects
			return m, m.gatherProjectCmd(list[m.pcursor].ID)
		}
	}
	return m, nil
}

func (m Model) loadSessions() tea.Msg {
	ss, err := m.svc.Sessions(m.proj.ID)
	if err != nil {
		return errMsg{err}
	}
	return sessionsMsg(ss)
}

func (m Model) prevListMode() mode {
	if m.prevMode == modeProjects {
		return modeProjects
	}
	return modeSessions
}

func (m Model) updateSessions(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		m.mode = modeProjects
		return m, m.loadProjects
	case "up", "k":
		if m.scursor > 0 {
			m.scursor--
		}
	case "down", "j":
		if m.scursor < len(m.sessions)-1 {
			m.scursor++
		}
	case " ":
		if len(m.sessions) > 0 {
			sid := m.sessions[m.scursor].ID
			m.selected[sid] = !m.selected[sid]
		}
	case "d":
		sids := m.selectedIDs()
		if len(sids) == 0 && len(m.sessions) > 0 {
			sids = []string{m.sessions[m.scursor].ID}
		}
		if len(sids) > 0 {
			m.prevMode = modeSessions
			return m, m.gatherSessionsCmd(sids)
		}
	case "c":
		m.prevMode = modeSessions
		return m, m.gatherClearCmd()
	case "r":
		if len(m.sessions) > 0 {
			m.input = nil
			m.mode = modeRename
		}
	case "p":
		if len(m.sessions) > 0 {
			return m, m.previewCmd(m.sessions[m.scursor].ID)
		}
	}
	return m, nil
}

func (m Model) selectedIDs() []string {
	var out []string
	for _, s := range m.sessions {
		if m.selected[s.ID] {
			out = append(out, s.ID)
		}
	}
	return out
}

func (m Model) gatherSessionsCmd(sids []string) tea.Cmd {
	pid := m.proj.ID
	return func() tea.Msg {
		p, err := m.svc.GatherSessions(pid, sids)
		if err != nil {
			return errMsg{err}
		}
		return planMsg(p)
	}
}

func (m Model) gatherClearCmd() tea.Cmd {
	pid := m.proj.ID
	return func() tea.Msg {
		p, err := m.svc.GatherClear(pid, app.DefaultClearRules())
		if err != nil {
			return errMsg{err}
		}
		return planMsg(p)
	}
}

func (m Model) gatherProjectCmd(pid string) tea.Cmd {
	return func() tea.Msg {
		p, err := m.svc.GatherProject(pid)
		if err != nil {
			return errMsg{err}
		}
		return planMsg(p)
	}
}

func (m Model) previewCmd(sid string) tea.Cmd {
	pid := m.proj.ID
	return func() tea.Msg {
		msgs, err := m.svc.Preview(pid, sid, 3)
		if err != nil {
			return errMsg{err}
		}
		return previewMsg(msgs)
	}
}

func (m Model) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "enter":
		plan := m.plan
		return m, func() tea.Msg {
			res, err := m.svc.Execute(plan)
			if err != nil {
				return errMsg{err}
			}
			return executedMsg(res)
		}
	case "n", "esc":
		m.mode = m.prevListMode()
	}
	return m, nil
}

func (m Model) updateRename(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		if len(m.sessions) == 0 || m.scursor >= len(m.sessions) {
			m.mode = modeSessions
			return m, nil
		}
		title := string(m.input)
		if strings.TrimSpace(title) == "" {
			m.mode = modeSessions
			return m, nil
		}
		pid, sid := m.proj.ID, m.sessions[m.scursor].ID
		return m, func() tea.Msg {
			if err := m.svc.Rename(pid, sid, title); err != nil {
				return errMsg{err}
			}
			return renamedMsg{}
		}
	case tea.KeyEsc:
		m.mode = modeSessions
	case tea.KeyBackspace:
		if len(m.input) > 0 {
			m.input = m.input[:len(m.input)-1]
		}
	case tea.KeyRunes:
		m.input = append(m.input, msg.Runes...)
	case tea.KeySpace:
		m.input = append(m.input, ' ')
	}
	return m, nil
}
