package messages

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/docker/docker-agent/pkg/tui/service"
)

// ---- findClosestRuneIndex tests ----

func TestFindClosestRuneIndex_ExactHit(t *testing.T) {
	t.Parallel()
	// Map: visual col 0 -> rune 0, col 2 -> rune 1, col 4 -> rune 2
	m := map[int]int{0: 0, 2: 1, 4: 2}
	assert.Equal(t, 0, findClosestRuneIndex(m, 0, 3))
	assert.Equal(t, 1, findClosestRuneIndex(m, 2, 3))
	assert.Equal(t, 2, findClosestRuneIndex(m, 4, 3))
}

func TestFindClosestRuneIndex_ForwardProbeAfterBorderChar(t *testing.T) {
	t.Parallel()
	// Border char at col 0 (width 1), content at col 1; visual col 0 has no entry.
	m := map[int]int{1: 0, 3: 1, 5: 2}
	// col 0 is a border: forward probe should find col 1 -> rune 0
	assert.Equal(t, 0, findClosestRuneIndex(m, 0, 3))
	// col 2 is between entries: forward probe finds col 3 -> rune 1
	assert.Equal(t, 1, findClosestRuneIndex(m, 2, 3))
}

func TestFindClosestRuneIndex_LongBorderRun(t *testing.T) {
	t.Parallel()
	// Simulate a long separator run "├──────────────────────┤" (>10 dashes)
	// Content rune is at col 20 (after 20 border columns).
	m := map[int]int{20: 0, 22: 1}
	// Old code capped forward probe at +10; col 0 would fall back to backward probe
	// and return maxRunes. New code probes forward without a cap.
	assert.Equal(t, 0, findClosestRuneIndex(m, 0, 2))
	assert.Equal(t, 0, findClosestRuneIndex(m, 15, 2))
}

func TestFindClosestRuneIndex_ColPastEndOfLine(t *testing.T) {
	t.Parallel()
	m := map[int]int{0: 0, 2: 1, 4: 2}
	// visualCol beyond all entries → forward probe finds nothing (nothing > 99),
	// backward probe finds col 4 → rune index 2 (the last content rune).
	assert.Equal(t, 2, findClosestRuneIndex(m, 99, 3))
	// Empty map → no entries at all → fallback returns maxRunes
	assert.Equal(t, 5, findClosestRuneIndex(map[int]int{}, 99, 5))
}

func TestFindClosestRuneIndex_EmptyMap(t *testing.T) {
	t.Parallel()
	m := map[int]int{}
	assert.Equal(t, 0, findClosestRuneIndex(m, 5, 0))
}

func TestFindClosestRuneIndex_AdjacentWideChars(t *testing.T) {
	t.Parallel()
	// Two 2-cell wide characters: 'A' at col 0, 'B' at col 2.
	// Col 1 is the second cell of 'A' — no map entry.
	m := map[int]int{0: 0, 2: 1}
	// col 1 → forward probe finds col 2 → rune 1
	assert.Equal(t, 1, findClosestRuneIndex(m, 1, 2))
	// col 0 → exact hit → rune 0
	assert.Equal(t, 0, findClosestRuneIndex(m, 0, 2))
}

func TestFindClosestRuneIndex_BackwardFallback(t *testing.T) {
	t.Parallel()
	// If forward probe finds nothing (col is after all entries) backward probe fires.
	m := map[int]int{0: 0, 2: 1}
	// col 3 → no forward hit (nothing > 3 in map) → backward finds col 2 → rune 1
	assert.Equal(t, 1, findClosestRuneIndex(m, 3, 2))
}

// ---- extractSelectedText tests via model ----

func newTestModel(t *testing.T) *model {
	t.Helper()
	sessionState := &service.SessionState{}
	m := NewScrollableView(80, 24, sessionState).(*model)
	m.SetSize(80, 24)
	return m
}

// setLines injects pre-rendered lines directly into the model's cache,
// bypassing ensureAllItemsRendered by marking the cache as clean.
func setLines(m *model, lines []string) {
	m.renderedLines = lines
	m.renderDirty = false
}

func TestExtractSelectedText_PlainText_NoTable(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	setLines(m, []string{"hello world"})
	m.selection.active = true
	m.selection.startLine = 0
	m.selection.startCol = 0
	m.selection.endLine = 0
	// endCol is inclusive (points AT 'd'), so all 11 chars should be selected.
	m.selection.endCol = 10

	got := m.extractSelectedText()
	assert.Equal(t, "hello world\n", got)
}

func TestExtractSelectedText_PlainText_Partial(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	setLines(m, []string{"hello world"})
	m.selection.active = true
	m.selection.startLine = 0
	m.selection.startCol = 6
	// endCol points AT 'r' (index 8 in "world"), inclusive.
	m.selection.endLine = 0
	m.selection.endCol = 9

	got := m.extractSelectedText()
	// "wor" (cols 6..9 inclusive = 4 chars "worl"? Let's check: h=0,e=1,l=2,l=3,o=4,' '=5,w=6,o=7,r=8,l=9,d=10)
	// startCol=6 -> 'w', endCol=9 -> 'l' (inclusive), endRuneIdx = idx of 'l' (rune 9) + 1 = 10 -> runes[6:10] = "worl"
	assert.Equal(t, "worl\n", got)
}

func TestExtractSelectedText_SingleColumnTable(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	// Simulate a single-column table row with box-drawing borders
	// "│ foo │" — border at col 0, space at col 1, 'f' at col 2, 'o' at col 3, 'o' at col 4,
	//             space at col 5, border at col 6
	setLines(m, []string{"│ foo │"})
	m.selection.active = true
	m.selection.startLine = 0
	// Select from inside the border (col 2 = 'f') to col 4 ('o'), inclusive
	m.selection.startCol = 2
	m.selection.endLine = 0
	m.selection.endCol = 4

	got := m.extractSelectedText()
	assert.Equal(t, "foo\n", got)
}

func TestExtractSelectedText_MultiColumnTable(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	// Simulate: "│ foo │ bar │"
	//            0123456789012
	// col: │=0, ' '=1, f=2, o=3, o=4, ' '=5, │=6, ' '=7, b=8, a=9, r=10, ' '=11, │=12
	setLines(m, []string{"│ foo │ bar │"})
	m.selection.active = true
	m.selection.startLine = 0
	// Select 'bar': startCol=8 ('b'), endCol=10 ('r') inclusive
	m.selection.startCol = 8
	m.selection.endLine = 0
	m.selection.endCol = 10

	got := m.extractSelectedText()
	assert.Equal(t, "bar\n", got)
}

func TestExtractSelectedText_MultilineSelection(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	setLines(m, []string{"hello", "world", "done"})
	m.selection.active = true
	m.selection.startLine = 0
	m.selection.startCol = 0
	m.selection.endLine = 1
	m.selection.endCol = 4 // inclusive 'd' in "world"

	got := m.extractSelectedText()
	assert.Equal(t, "hello\nworld\n", got)
}

func TestExtractSelectedText_MultilineWithMiddleLine(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	setLines(m, []string{"first", "middle line", "last"})
	m.selection.active = true
	m.selection.startLine = 0
	m.selection.startCol = 0
	m.selection.endLine = 2
	m.selection.endCol = 3 // inclusive 't' in "last"

	got := m.extractSelectedText()
	assert.Equal(t, "first\nmiddle line\nlast\n", got)
}

func TestExtractSelectedText_WideCJKCharsInCell(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	// "│ 你好 │": │ at col 0, space at col 1,
	// '你' at col 2 (width 2), '好' at col 4 (width 2), space at col 6, │ at col 7
	setLines(m, []string{"│ 你好 │"})
	m.selection.active = true
	m.selection.startLine = 0
	// Select from col 2 ('你') to col 5 (second cell of '好'), inclusive.
	m.selection.startCol = 2
	m.selection.endLine = 0
	m.selection.endCol = 5

	got := m.extractSelectedText()
	assert.Equal(t, "你好\n", got)
}

func TestExtractSelectedText_SelectionOnBorderChars(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	// "│ foo │" — if user clicks the border at col 0 and drags to col 6 (border)
	// we should still get "foo" (borders are stripped, probing finds content runes)
	setLines(m, []string{"│ foo │"})
	m.selection.active = true
	m.selection.startLine = 0
	m.selection.startCol = 0 // on border '│'
	m.selection.endLine = 0
	m.selection.endCol = 6 // on trailing border '│'

	got := m.extractSelectedText()
	assert.Equal(t, "foo\n", got)
}

func TestExtractSelectedText_NoSelection(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	setLines(m, []string{"hello"})
	m.selection.active = false

	got := m.extractSelectedText()
	assert.Empty(t, got)
}

func TestExtractSelectedText_BorderOnlyLine(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	// A separator row like "├──────┤" should produce empty text
	setLines(m, []string{"├──────┤"})
	m.selection.active = true
	m.selection.startLine = 0
	m.selection.startCol = 0
	m.selection.endLine = 0
	m.selection.endCol = 7

	got := m.extractSelectedText()
	// No content runes → empty trimmed text, just the trailing newline from the loop
	assert.Equal(t, "\n", got)
}
