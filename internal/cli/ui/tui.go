package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"

	"github.com/matzehuels/stacktower/pkg/core/deps"
	"github.com/matzehuels/stacktower/pkg/core/deps/languages"
	"github.com/matzehuels/stacktower/pkg/integrations/github"
)

// TUIHints contains shared TUI chrome for navigation hints.
const TUIHints = "↑/↓ navigate  ⏎ select  q quit"

// List styles
var (
	ListSelectedStyle = lipgloss.NewStyle().Bold(true).Foreground(ColorPurple)
	ListNormalStyle   = lipgloss.NewStyle().Foreground(ColorWhite)
	ListDimStyle      = lipgloss.NewStyle().Foreground(ColorDim)

	TUITableBorder = lipgloss.NewStyle().Foreground(ColorDim)
	TUIHeaderStyle = lipgloss.NewStyle().Foreground(ColorGray).Bold(true)
)

// =============================================================================
// RepoListModel - Interactive repository selection
// =============================================================================

// RepoSelection holds the result of the repo selection.
type RepoSelection struct {
	Repo *github.RepoWithManifests
}

// RepoListModel is the bubbletea model for interactive repo selection.
type RepoListModel struct {
	Repos    []github.RepoWithManifests
	Cursor   int
	Selected *RepoSelection
	Height   int
	Offset   int
}

// NewRepoListModel creates a new repo list model.
func NewRepoListModel(repos []github.RepoWithManifests) RepoListModel {
	return RepoListModel{
		Repos:  repos,
		Cursor: 0,
		Height: 15,
		Offset: 0,
	}
}

func (m RepoListModel) Init() tea.Cmd {
	return nil
}

func (m RepoListModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		case "up", "k":
			if m.Cursor > 0 {
				m.Cursor--
				if m.Cursor < m.Offset {
					m.Offset = m.Cursor
				}
			}
		case "down", "j":
			if m.Cursor < len(m.Repos)-1 {
				m.Cursor++
				if m.Cursor >= m.Offset+m.Height {
					m.Offset = m.Cursor - m.Height + 1
				}
			}
		case "enter":
			repo := m.Repos[m.Cursor]
			if len(repo.Manifests) == 0 {
				return m, nil
			}
			m.Selected = &RepoSelection{Repo: &repo}
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		m.Height = msg.Height - 6
		if m.Height < 5 {
			m.Height = 5
		}
	}
	return m, nil
}

func (m RepoListModel) View() string {
	var b strings.Builder

	b.WriteString(StyleTitle.Render("Select Repository"))
	b.WriteString("\n")
	b.WriteString(ListDimStyle.Render(TUIHints))
	b.WriteString("\n\n")

	end := m.Offset + m.Height
	if end > len(m.Repos) {
		end = len(m.Repos)
	}

	rows := [][]string{}
	for i := m.Offset; i < end; i++ {
		r := m.Repos[i]
		hasManifests := len(r.Manifests) > 0

		cursor := "  "
		if i == m.Cursor {
			cursor = "▸ "
		}

		visibility := "✓"
		if r.Repo.Private {
			visibility = ""
		}

		lang := ""
		if hasManifests {
			lang = r.Manifests[0].Language
		} else if r.Repo.Language != "" {
			lang = deps.NormalizeLanguageName(r.Repo.Language, languages.All)
		}
		if lang == "" {
			lang = "—"
		}

		manifestStr := "—"
		if hasManifests {
			names := make([]string, len(r.Manifests))
			for j, mf := range r.Manifests {
				names[j] = mf.Name
			}
			manifestStr = strings.Join(names, ", ")
		}

		updated := formatRelativeTime(r.Repo.UpdatedAt)
		rows = append(rows, []string{cursor, r.Repo.FullName, lang, visibility, updated, manifestStr})
	}

	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(TUITableBorder).
		Headers("", "Repository", "Lang", "Public", "Updated", "Manifests").
		Rows(rows...).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == -1 {
				return TUIHeaderStyle
			}

			actualIdx := m.Offset + row
			if actualIdx >= len(m.Repos) {
				return lipgloss.NewStyle()
			}
			r := m.Repos[actualIdx]
			hasManifests := len(r.Manifests) > 0
			isCurrent := actualIdx == m.Cursor

			base := lipgloss.NewStyle()
			if col == 3 || col == 4 {
				if isCurrent {
					base = base.Foreground(ColorGray)
				} else {
					base = base.Foreground(ColorDim)
				}
			}

			if isCurrent {
				if hasManifests {
					if col != 3 && col != 4 {
						return base.Foreground(ColorGreen).Bold(true)
					}
					return base.Bold(true)
				}
				return base.Foreground(ColorDim).Bold(true)
			} else if hasManifests {
				if col != 3 && col != 4 {
					return base.Foreground(ColorGreen)
				}
				return base
			}
			return base.Foreground(ColorDim)
		})

	b.WriteString(t.Render())
	b.WriteString("\n\n")
	b.WriteString(ListDimStyle.Render(fmt.Sprintf("  [%d/%d]", m.Cursor+1, len(m.Repos))))

	return b.String()
}

// =============================================================================
// ManifestListModel - Interactive manifest file selection
// =============================================================================

// ManifestListModel is the bubbletea model for interactive manifest selection.
type ManifestListModel struct {
	Manifests []github.ManifestFile
	Cursor    int
	Selected  *github.ManifestFile
}

// NewManifestListModel creates a new manifest list model.
func NewManifestListModel(manifests []github.ManifestFile) ManifestListModel {
	return ManifestListModel{Manifests: manifests}
}

func (m ManifestListModel) Init() tea.Cmd {
	return nil
}

func (m ManifestListModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		case "up", "k":
			if m.Cursor > 0 {
				m.Cursor--
			}
		case "down", "j":
			if m.Cursor < len(m.Manifests)-1 {
				m.Cursor++
			}
		case "enter":
			m.Selected = &m.Manifests[m.Cursor]
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m ManifestListModel) View() string {
	var b strings.Builder

	b.WriteString(StyleTitle.Render("Select Manifest File"))
	b.WriteString("\n")
	b.WriteString(ListDimStyle.Render(TUIHints))
	b.WriteString("\n\n")

	rows := make([][]string, 0, len(m.Manifests))
	for i, mf := range m.Manifests {
		cursor := "  "
		if i == m.Cursor {
			cursor = "▸ "
		}

		supported := deps.IsManifestSupported(mf.Name, languages.All)
		status := IconSuccess
		if !supported {
			status = IconWarning
		}

		rows = append(rows, []string{cursor, status, mf.Name, mf.Language})
	}

	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(TUITableBorder).
		Headers("", "", "Manifest", "Language").
		Rows(rows...).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == -1 {
				return TUIHeaderStyle
			}
			if row < 0 || row >= len(m.Manifests) {
				return lipgloss.NewStyle()
			}

			mf := m.Manifests[row]
			supported := deps.IsManifestSupported(mf.Name, languages.All)
			isCurrent := row == m.Cursor

			if col == 1 {
				if supported {
					return lipgloss.NewStyle().Foreground(ColorGreen)
				}
				return lipgloss.NewStyle().Foreground(ColorYellow)
			}

			if isCurrent {
				if supported {
					return lipgloss.NewStyle().Foreground(ColorGreen).Bold(true)
				}
				return lipgloss.NewStyle().Foreground(ColorDim).Bold(true)
			}
			if !supported {
				return lipgloss.NewStyle().Foreground(ColorDim)
			}
			return lipgloss.NewStyle().Foreground(ColorWhite)
		})

	b.WriteString(t.Render())
	b.WriteString("\n\n")
	b.WriteString(ListDimStyle.Render(fmt.Sprintf("  %s supported   %s not yet supported",
		StyleIconSuccess.Render(IconSuccess), StyleIconWarning.Render(IconWarning))))
	b.WriteString("\n")

	return b.String()
}

// =============================================================================
// RefListModel - Interactive git ref selection (branches + tags)
// =============================================================================

// RefItem represents a branch or tag for interactive selection.
type RefItem struct {
	Name      string
	Type      string // "branch" or "tag"
	Commit    string
	IsDefault bool
}

// RefListModel is the bubbletea model for interactive ref selection.
type RefListModel struct {
	Items    []RefItem
	Cursor   int
	Selected *RefItem
	Filter   string
	Filtered []int // indices into Items matching the filter
	Height   int
	Offset   int
}

// NewRefListModel creates a ref list with the default branch first.
func NewRefListModel(branches []github.Branch, tags []github.Tag, defaultBranch string) RefListModel {
	var items []RefItem

	// Default branch always first
	for _, b := range branches {
		if b.Name == defaultBranch {
			items = append(items, RefItem{
				Name: b.Name, Type: "branch", Commit: b.Commit, IsDefault: true,
			})
			break
		}
	}

	for _, b := range branches {
		if b.Name == defaultBranch {
			continue
		}
		items = append(items, RefItem{
			Name: b.Name, Type: "branch", Commit: b.Commit,
		})
	}

	for _, t := range tags {
		items = append(items, RefItem{
			Name: t.Name, Type: "tag", Commit: t.Commit,
		})
	}

	m := RefListModel{
		Items:  items,
		Height: 15,
	}
	m.rebuildFilter()
	return m
}

func (m *RefListModel) rebuildFilter() {
	m.Filtered = m.Filtered[:0]
	lower := strings.ToLower(m.Filter)
	for i, item := range m.Items {
		if lower == "" || strings.Contains(strings.ToLower(item.Name), lower) {
			m.Filtered = append(m.Filtered, i)
		}
	}
	if m.Cursor >= len(m.Filtered) {
		m.Cursor = max(0, len(m.Filtered)-1)
	}
	m.Offset = 0
}

func (m RefListModel) Init() tea.Cmd {
	return nil
}

func (m RefListModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			return m, tea.Quit
		case "up", "ctrl+p":
			if m.Cursor > 0 {
				m.Cursor--
				if m.Cursor < m.Offset {
					m.Offset = m.Cursor
				}
			}
		case "down", "ctrl+n":
			if m.Cursor < len(m.Filtered)-1 {
				m.Cursor++
				if m.Cursor >= m.Offset+m.Height {
					m.Offset = m.Cursor - m.Height + 1
				}
			}
		case "enter":
			if len(m.Filtered) > 0 {
				idx := m.Filtered[m.Cursor]
				item := m.Items[idx]
				m.Selected = &item
			}
			return m, tea.Quit
		case "backspace":
			if len(m.Filter) > 0 {
				m.Filter = m.Filter[:len(m.Filter)-1]
				m.rebuildFilter()
			}
		default:
			if len(msg.String()) == 1 {
				ch := msg.String()[0]
				if ch >= 32 && ch < 127 {
					m.Filter += string(ch)
					m.rebuildFilter()
				}
			}
		}
	case tea.WindowSizeMsg:
		m.Height = msg.Height - 8
		if m.Height < 5 {
			m.Height = 5
		}
	}
	return m, nil
}

func (m RefListModel) View() string {
	var b strings.Builder

	b.WriteString(StyleTitle.Render("Select Git Reference"))
	b.WriteString("\n")
	b.WriteString(ListDimStyle.Render("↑/↓ navigate  ⏎ select  type to filter  esc quit"))
	b.WriteString("\n")

	if m.Filter != "" {
		fmt.Fprintf(&b, "  filter: %s\n", StyleHighlight.Render(m.Filter))
	}
	b.WriteString("\n")

	if len(m.Filtered) == 0 {
		b.WriteString(ListDimStyle.Render("  No matching refs"))
		b.WriteString("\n")
		return b.String()
	}

	end := m.Offset + m.Height
	if end > len(m.Filtered) {
		end = len(m.Filtered)
	}

	prevType := ""
	for vi := m.Offset; vi < end; vi++ {
		item := m.Items[m.Filtered[vi]]

		// Section headers
		if item.Type != prevType {
			if prevType != "" {
				b.WriteString("\n")
			}
			label := "Branches"
			if item.Type == "tag" {
				label = "Tags"
			}
			b.WriteString("  " + ListDimStyle.Render(label) + "\n")
			prevType = item.Type
		}

		cursor := "    "
		if vi == m.Cursor {
			cursor = "  ▸ "
		}

		name := item.Name
		suffix := ""
		if item.IsDefault {
			suffix = " (default)"
		}

		short := ""
		if item.Commit != "" && len(item.Commit) >= 7 {
			short = item.Commit[:7]
		}

		if vi == m.Cursor {
			line := fmt.Sprintf("%s%-30s %s", cursor, name+suffix, ListDimStyle.Render(short))
			b.WriteString(ListSelectedStyle.Render(line))
		} else if item.IsDefault {
			line := fmt.Sprintf("%s%s%s  %s", cursor,
				StyleSuccess.Render(name),
				ListDimStyle.Render(suffix),
				ListDimStyle.Render(short))
			b.WriteString(line)
		} else {
			line := fmt.Sprintf("%s%-30s %s", cursor, name, ListDimStyle.Render(short))
			b.WriteString(ListNormalStyle.Render(line))
		}
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(ListDimStyle.Render(fmt.Sprintf("  [%d/%d]", m.Cursor+1, len(m.Filtered))))

	return b.String()
}

// =============================================================================
// Helpers
// =============================================================================

func formatRelativeTime(s string) string {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return s
	}

	now := time.Now()
	diff := now.Sub(t)

	switch {
	case diff < time.Hour:
		return fmt.Sprintf("%dm ago", int(diff.Minutes()))
	case diff < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(diff.Hours()))
	case diff < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(diff.Hours()/24))
	default:
		return t.Format("Jan 2, 2006")
	}
}
