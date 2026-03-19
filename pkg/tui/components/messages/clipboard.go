package messages

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/atotto/clipboard"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"

	"github.com/docker/docker-agent/pkg/tui/components/notification"
)

// boxDrawingChars contains Unicode box-drawing characters used by lipgloss borders.
// These need to be stripped when copying text to clipboard.
var boxDrawingChars = map[rune]bool{
	// Thick border characters
	'┃': true, '━': true, '┏': true, '┓': true, '┗': true, '┛': true,
	// Normal border characters
	'│': true, '─': true, '┌': true, '┐': true, '└': true, '┘': true,
	// Double border characters
	'║': true, '═': true, '╔': true, '╗': true, '╚': true, '╝': true,
	// Rounded border characters
	'╭': true, '╮': true, '╯': true, '╰': true,
	// Block border characters
	'█': true, '▀': true, '▄': true,
	// Additional box-drawing characters that might appear
	'┣': true, '┫': true, '┳': true, '┻': true, '╋': true,
	'├': true, '┤': true, '┬': true, '┴': true, '┼': true,
	'╠': true, '╣': true, '╦': true, '╩': true, '╬': true,
}

// stripBorderChars removes box-drawing characters from text.
// This is used when copying selected text to clipboard to avoid
// including visual border decorations in the copied content.
func stripBorderChars(s string) string {
	var result strings.Builder
	result.Grow(len(s))
	for _, r := range s {
		if !boxDrawingChars[r] {
			result.WriteRune(r)
		}
	}
	return result.String()
}

// isWordChar returns true if the rune is a word character (letter, digit, or underscore)
func isWordChar(r rune) bool {
	return (r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9') ||
		r == '_' ||
		r >= 0x80 // Include non-ASCII characters (unicode letters, etc.)
}

// displayWidthToRuneIndex converts a display width to a rune index
func displayWidthToRuneIndex(s string, targetWidth int) int {
	if targetWidth <= 0 {
		return 0
	}

	runes := []rune(s)
	currentWidth := 0

	for i, r := range runes {
		if currentWidth >= targetWidth {
			return i
		}
		currentWidth += runewidth.RuneWidth(r)
	}

	return len(runes)
}

// runeIndexToDisplayWidth converts a rune index to display width
func runeIndexToDisplayWidth(s string, runeIdx int) int {
	runes := []rune(s)
	if runeIdx > len(runes) {
		runeIdx = len(runes)
	}
	width := 0
	for i := range runeIdx {
		width += runewidth.RuneWidth(runes[i])
	}
	return width
}

// extractSelectedText extracts the currently selected text from rendered content
func (m *model) extractSelectedText() string {
	if !m.selection.active {
		return ""
	}

	m.ensureAllItemsRendered()
	lines := m.renderedLines
	startLine, startCol, endLine, endCol := m.selection.normalized()

	if startLine < 0 || startLine >= len(lines) {
		return ""
	}
	if endLine >= len(lines) {
		endLine = len(lines) - 1
	}

	var result strings.Builder
	for i := startLine; i <= endLine && i < len(lines); i++ {
		originalLine := lines[i]
		// Strip ANSI codes first to get the displayed text with borders
		plainLine := ansi.Strip(originalLine)
		// Strip border characters to get the actual text content
		line := stripBorderChars(plainLine)
		runes := []rune(line)

		// Map visual column positions from the plain line (with borders) to the
		// stripped line (without borders) by tracking which runes correspond to
		// which visual columns. Ranging over a string already iterates rune-by-rune.
		visualToRune := make(map[int]int)
		visualCol := 0
		lineRuneIdx := 0

		for _, r := range plainLine {
			if !boxDrawingChars[r] {
				// This rune is kept in the stripped line
				visualToRune[visualCol] = lineRuneIdx
				lineRuneIdx++
			}
			visualCol += runewidth.RuneWidth(r)
		}

		// Find the rune index for the start column (used as an inclusive lower bound).
		startRuneIdx := findClosestRuneIndex(visualToRune, startCol, len(runes))

		// Find the rune index for the end column.
		// Terminal mouse events report the column the cursor is ON (inclusive).
		// findClosestRuneIndex returns the index of the rune AT endCol, but we use
		// the result as an exclusive upper bound in runes[:endRuneIdx]. To include
		// the character the user released on, we advance by 1.
		endRuneIdx := findClosestRuneIndex(visualToRune, endCol, len(runes))
		if endRuneIdx < len(runes) {
			endRuneIdx++
		}

		var lineText string
		switch i {
		case startLine:
			if startLine == endLine {
				if startRuneIdx < len(runes) && startRuneIdx < endRuneIdx {
					lineText = strings.TrimSpace(string(runes[startRuneIdx:endRuneIdx]))
				}
				break
			}
			// First line: from startCol to end
			if startRuneIdx < len(runes) {
				lineText = strings.TrimSpace(string(runes[startRuneIdx:]))
			}
		case endLine:
			// Last line: from start to endCol (endRuneIdx is already exclusive)
			lineText = strings.TrimSpace(string(runes[:endRuneIdx]))
		default:
			// Middle lines: entire line
			lineText = strings.TrimSpace(line)
		}

		if lineText != "" {
			result.WriteString(lineText)
		}
		result.WriteString("\n")
	}

	return result.String()
}

// findClosestRuneIndex returns the index into the stripped-line rune slice that
// corresponds to the given visual column in the original (bordered) line.
//
// The visualToRune map maps each content rune's start visual column to its index
// in the stripped line. When no entry exists at visualCol (e.g., the column falls
// inside a border character or a multi-cell wide character), the function probes
// forward for the next content rune, then backward.
//
// Usage semantics:
//   - When used as an inclusive start index (runes[startRuneIdx:]), use the
//     result directly.
//   - When used as an exclusive end index (runes[:endRuneIdx]), the caller must
//     add 1 after calling this function if endCol is inclusive (pointing AT the
//     last selected character, as terminal mouse-release events report).
func findClosestRuneIndex(visualToRune map[int]int, visualCol, maxRunes int) int {
	// Try exact match first
	if runeIdx, ok := visualToRune[visualCol]; ok {
		return runeIdx
	}

	// Probe forward: find the next content rune after visualCol.
	// No fixed cap — iterate until we find a content rune or exhaust the map.
	// This handles arbitrarily long border runs (e.g., "├──────────────────┤").
	maxVisualCol := 0
	for col := range visualToRune {
		if col > maxVisualCol {
			maxVisualCol = col
		}
	}
	for col := visualCol + 1; col <= maxVisualCol; col++ {
		if runeIdx, ok := visualToRune[col]; ok {
			return runeIdx
		}
	}

	// Find the previous available rune index
	for col := visualCol - 1; col >= 0; col-- {
		if runeIdx, ok := visualToRune[col]; ok {
			return runeIdx
		}
	}

	// Fallback: return the last rune index
	return maxRunes
}

// copySelectionToClipboard copies the currently selected text to clipboard
func (m *model) copySelectionToClipboard() tea.Cmd {
	if !m.selection.active {
		return nil
	}

	selectedText := strings.TrimSpace(m.extractSelectedText())
	if selectedText == "" {
		return nil
	}

	return copyTextToClipboard(selectedText)
}

// copySelectedMessageToClipboard copies the content of the selected message to clipboard
func (m *model) copySelectedMessageToClipboard() tea.Cmd {
	if m.selectedMessageIndex < 0 || m.selectedMessageIndex >= len(m.messages) {
		return nil
	}

	msg := m.messages[m.selectedMessageIndex]
	content := msg.Content

	if content == "" {
		return nil
	}

	return copyTextToClipboard(content)
}

// copyTextToClipboard copies text to the system clipboard
func copyTextToClipboard(text string) tea.Cmd {
	return tea.Sequence(
		func() tea.Msg {
			_ = clipboard.WriteAll(text)
			return nil
		},
		tea.SetClipboard(text),
		notification.SuccessCmd("Text copied to clipboard."),
	)
}

// scheduleDebouncedCopy schedules a copy after a delay, allowing triple-click to cancel it.
func (m *model) scheduleDebouncedCopy() tea.Cmd {
	m.selection.pendingCopyID++
	copyID := m.selection.pendingCopyID
	return tea.Tick(400*time.Millisecond, func(time.Time) tea.Msg {
		return DebouncedCopyMsg{ClickID: copyID}
	})
}

// handleDebouncedCopy executes copy only if no subsequent click invalidated it.
func (m *model) handleDebouncedCopy(msg DebouncedCopyMsg) tea.Cmd {
	if msg.ClickID == m.selection.pendingCopyID {
		return m.copySelectionToClipboard()
	}
	return nil
}
