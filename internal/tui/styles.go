package tui

import "github.com/charmbracelet/lipgloss"

var (
	// Colors
	colorPrimary   = lipgloss.Color("#7C3AED") // purple
	colorSecondary = lipgloss.Color("#06B6D4") // cyan
	colorUser      = lipgloss.Color("#22C55E") // green
	colorAssistant = lipgloss.Color("#3B82F6") // blue
	colorTool      = lipgloss.Color("#EAB308") // yellow
	colorThinking  = lipgloss.Color("#A855F7") // purple
	colorMuted     = lipgloss.Color("#6B7280") // gray
	colorBorder    = lipgloss.Color("#374151") // dark gray
	colorSelected  = lipgloss.Color("#1E1B4B") // deep purple bg
	colorError     = lipgloss.Color("#EF4444") // red

	// Styles
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorPrimary).
			PaddingLeft(1)

	selectedStyle = lipgloss.NewStyle().
			Background(colorSelected).
			Foreground(lipgloss.Color("#FFFFFF")).
			Bold(true)

	normalItemStyle = lipgloss.NewStyle().
			PaddingLeft(2)

	projectHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorSecondary).
				PaddingLeft(1)

	userLabelStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorUser)

	assistantLabelStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorAssistant)

	toolLabelStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorTool)

	thinkingLabelStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorThinking)

	mutedStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	toolBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorTool).
			Padding(0, 1)

	statusBarStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#1F2937")).
			Foreground(lipgloss.Color("#9CA3AF")).
			Padding(0, 1)

	helpKeyStyle = lipgloss.NewStyle().
			Foreground(colorPrimary).
			Bold(true)

	helpDescStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorPrimary).
			Border(lipgloss.NormalBorder(), false, false, true, false).
			BorderForeground(colorBorder).
			PaddingLeft(1).
			MarginBottom(1)
)
