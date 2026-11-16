package tui

import "github.com/charmbracelet/bubbles/key"

type keyMap struct {
	Up        key.Binding
	Down      key.Binding
	Left      key.Binding
	Right     key.Binding
	Enter     key.Binding
	Search    key.Binding
	Quit      key.Binding
	Copy      key.Binding
	Resume    key.Binding
	Toggle    key.Binding // thinking blocks
	Privacy   key.Binding
	Help      key.Binding
	Tab       key.Binding
	ShiftTab  key.Binding
	Escape    key.Binding
	PageUp    key.Binding
	PageDown  key.Binding
	Refresh   key.Binding
	Logs      key.Binding
	Flag      key.Binding
	Dashboard key.Binding
}

var keys = keyMap{
	Up:        key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
	Down:      key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
	Left:      key.NewBinding(key.WithKeys("left"), key.WithHelp("←", "collapse")),
	Right:     key.NewBinding(key.WithKeys("right"), key.WithHelp("→", "expand")),
	Enter:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
	Search:    key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
	Quit:      key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	Copy:      key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "copy ID")),
	Resume:    key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "resume")),
	Toggle:    key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "thinking")),
	Privacy:   key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "privacy")),
	Help:      key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	Tab:       key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next pane")),
	ShiftTab:  key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("S-tab", "prev pane")),
	Escape:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
	PageUp:    key.NewBinding(key.WithKeys("pgup"), key.WithHelp("pgup", "page up")),
	PageDown:  key.NewBinding(key.WithKeys("pgdown"), key.WithHelp("pgdown", "page down")),
	Refresh:   key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("ctrl+r", "refresh")),
	Logs:      key.NewBinding(key.WithKeys("l"), key.WithHelp("l", "view logs")),
	Flag:      key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "flag/monitor")),
	Dashboard: key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "dashboard")),
}
