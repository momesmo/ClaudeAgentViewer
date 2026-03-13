package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/user/claudevu/internal/data"
)

// subagent border styles by type
var (
	exploreBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.ThickBorder(), false, false, false, true).
				BorderForeground(colorSecondary).
				PaddingLeft(1)

	planBorderStyle = lipgloss.NewStyle().
			Border(lipgloss.ThickBorder(), false, false, false, true).
			BorderForeground(colorTool).
			PaddingLeft(1)

	generalBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.ThickBorder(), false, false, false, true).
				BorderForeground(colorAssistant).
				PaddingLeft(1)
)

func subagentBorderFor(agentType string) lipgloss.Style {
	switch agentType {
	case "Explore":
		return exploreBorderStyle
	case "Plan":
		return planBorderStyle
	default:
		return generalBorderStyle
	}
}

// renderMessage converts a SessionMessage into styled terminal output
func renderMessage(msg data.SessionMessage, width int, showThinking bool, privacyMode bool) string {
	if width < 20 {
		width = 20
	}
	contentWidth := width - 4

	var sb strings.Builder

	switch msg.Type {
	case "user":
		sb.WriteString(userLabelStyle.Render("USER") + "\n")
		content := msg.Content
		if privacyMode {
			content = data.RedactSecrets(content)
		}
		sb.WriteString(wrapText(content, contentWidth))
		sb.WriteString("\n")

	case "assistant":
		for _, block := range msg.Blocks {
			switch block.Type {
			case "text":
				if block.Text == "" {
					continue
				}
				sb.WriteString(assistantLabelStyle.Render("ASSISTANT") + "\n")
				text := block.Text
				if privacyMode {
					text = data.RedactSecrets(text)
				}
				sb.WriteString(wrapText(text, contentWidth))
				sb.WriteString("\n")

			case "thinking":
				if !showThinking {
					sb.WriteString(thinkingLabelStyle.Render("Thinking") + mutedStyle.Render(" (hidden, press t)") + "\n")
					continue
				}
				sb.WriteString(thinkingLabelStyle.Render("Thinking") + "\n")
				text := block.Text
				if privacyMode {
					text = data.RedactSecrets(text)
				}
				sb.WriteString(mutedStyle.Render(wrapText(text, contentWidth)))
				sb.WriteString("\n")

			case "tool_use":
				if block.ToolName == "Agent" {
					// Render Agent header with type badge
					renderAgentToolUse(&sb, block, contentWidth)
				} else {
					label := fmt.Sprintf("TOOL: %s", block.ToolName)
					sb.WriteString(toolLabelStyle.Render(label) + "\n")
					if block.ToolInput != "" {
						input := block.ToolInput
						if privacyMode {
							input = data.RedactSecrets(input)
						}
						if len(input) > 300 {
							input = input[:300] + "..."
						}
						boxContent := wrapText(input, contentWidth-4)
						sb.WriteString(toolBoxStyle.Render(boxContent))
						sb.WriteString("\n")
					}
				}

			case "tool_result":
				if block.Text != "" {
					text := block.Text
					if privacyMode {
						text = data.RedactSecrets(text)
					}
					sb.WriteString(mutedStyle.Render(wrapText(text, contentWidth)))
					sb.WriteString("\n")
				}
			}
		}
	}

	return sb.String()
}

// renderAgentToolUse renders an Agent tool call with type badge and description
func renderAgentToolUse(sb *strings.Builder, block data.ContentBlock, width int) {
	emoji := data.SubagentTypeEmoji(block.AgentType)
	agentType := block.AgentType
	if agentType == "" {
		agentType = "Agent"
	}
	desc := block.AgentDescription
	if desc == "" {
		desc = "subagent task"
	}

	header := fmt.Sprintf("%s AGENT [%s]: %s", emoji, agentType, desc)
	sb.WriteString(toolLabelStyle.Render(header) + "\n")
}

// renderSubagentTranscript renders a subagent's transcript with a colored left border
func renderSubagentTranscript(sa data.SubagentInfo, width int, showThinking bool, privacyMode bool) string {
	var sb strings.Builder

	innerWidth := width - 6
	for _, msg := range sa.Messages {
		rendered := renderMessage(msg, innerWidth, showThinking, privacyMode)
		if rendered != "" {
			sb.WriteString(rendered)
		}
	}

	borderStyle := subagentBorderFor(sa.AgentType)
	return borderStyle.Width(width - 2).Render(sb.String())
}

// wrapText wraps text to fit within a given width
func wrapText(text string, width int) string {
	if width <= 0 {
		return text
	}

	var result strings.Builder
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if i > 0 {
			result.WriteString("\n")
		}
		if len(line) <= width {
			result.WriteString(line)
			continue
		}
		// Simple word wrap
		words := strings.Fields(line)
		lineLen := 0
		for j, word := range words {
			wlen := len(word)
			if j > 0 && lineLen+1+wlen > width {
				result.WriteString("\n")
				lineLen = 0
			} else if j > 0 {
				result.WriteString(" ")
				lineLen++
			}
			result.WriteString(word)
			lineLen += wlen
		}
	}
	return result.String()
}
