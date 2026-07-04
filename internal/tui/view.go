package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"midden/internal/format"
)

var (
	headerStyle = lipgloss.NewStyle().Bold(true)
	dimStyle    = lipgloss.NewStyle().Faint(true)
	cursorStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
)

func (m Model) View() string {
	if m.err != nil {
		return "error: " + m.err.Error() + "\n(any key to go back · ctrl+c to quit)\n"
	}
	switch m.mode {
	case modeSessions, modeConfirm, modePreview, modeRename:
		return m.viewSessions() // fleshed out in Task 15
	default:
		return m.viewProjects()
	}
}

func (m Model) viewProjects() string {
	var b strings.Builder
	var total int64
	for _, p := range m.projects {
		total += p.SizeBytes
	}
	reclaim := "…"
	if m.reclaim >= 0 {
		reclaim = "~" + format.Size(m.reclaim)
	}
	b.WriteString(headerStyle.Render(fmt.Sprintf(
		"AI Session Manager — %d projects · %s · reclaimable %s", len(m.projects), format.Size(total), reclaim)) + "\n\n")
	now := time.Now()
	for i, p := range m.filtered() {
		prefix := "  "
		line := fmt.Sprintf("%-30s %3d sessions  %9s  %s", p.Name, p.Sessions, format.Size(p.SizeBytes), format.Ago(p.LastActive, now))
		if i == m.pcursor {
			b.WriteString(cursorStyle.Render("> "+line) + "\n")
		} else {
			b.WriteString(prefix + line + "\n")
		}
	}
	if m.filtering {
		b.WriteString("\n/" + m.filter + "▌\n")
	}
	if m.status != "" {
		b.WriteString("\n" + m.status + "\n")
	}
	b.WriteString("\n" + dimStyle.Render("↑↓ move · enter open · d delete project · / filter · q quit") + "\n")
	return b.String()
}

func (m Model) viewSessions() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render(fmt.Sprintf("%s — %d sessions", m.proj.Name, len(m.sessions))) + "\n\n")
	now := time.Now()
	for i, s := range m.sessions {
		mark := "  "
		if m.selected[s.ID] {
			mark = "✓ "
		}
		active := ""
		if s.Active {
			active = " ⚡"
		}
		line := fmt.Sprintf("%s%-50s %9s %5d msgs  %s%s",
			mark, truncate(s.Title, 50), format.Size(s.SizeBytes), s.Messages, format.Ago(s.Modified, now), active)
		if i == m.scursor {
			b.WriteString(cursorStyle.Render("> "+line) + "\n")
		} else {
			b.WriteString("  " + line + "\n")
		}
	}
	switch m.mode {
	case modeConfirm:
		b.WriteString("\n" + m.viewConfirm())
	case modePreview:
		b.WriteString("\n" + m.viewPreview())
	case modeRename:
		b.WriteString("\nrename: " + string(m.input) + "▌  (enter save · esc cancel)\n")
	}
	if m.status != "" {
		b.WriteString("\n" + m.status + "\n")
	}
	b.WriteString("\n" + dimStyle.Render("space select · d delete · c clean · r rename · p preview · esc back") + "\n")
	return b.String()
}

func (m Model) viewConfirm() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render(fmt.Sprintf("Move to trash: %s (%s)", m.plan.Label, format.Size(m.plan.SizeBytes))) + "\n")
	max := len(m.plan.Details)
	if max > 10 {
		max = 10
	}
	for _, d := range m.plan.Details[:max] {
		b.WriteString("  " + d + "\n")
	}
	if len(m.plan.Details) > 10 {
		b.WriteString(fmt.Sprintf("  … and %d more\n", len(m.plan.Details)-10))
	}
	if m.plan.ActiveCount > 0 {
		b.WriteString(fmt.Sprintf("⚠ includes %d ACTIVE session(s)\n", m.plan.ActiveCount))
	}
	b.WriteString(dimStyle.Render("y confirm · n cancel") + "\n")
	return b.String()
}

func (m Model) viewPreview() string {
	var b strings.Builder
	for _, msg := range m.preview {
		b.WriteString(fmt.Sprintf("%s: %s\n", msg.Role, msg.Text))
	}
	b.WriteString(dimStyle.Render("any key to close") + "\n")
	return b.String()
}

func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
