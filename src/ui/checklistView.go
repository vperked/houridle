//go:build windows

package ui

import (
	"fmt"
	"unicode/utf8"
)

// checklistChrome is the lines that aren't list rows: banner, search box,
// "more" line, footer, message and spacing.
const checklistChrome = 11

// Render draws the checklist for a console of termW × termH.
func (c *Checklist) Render(termW, termH int) string {
	width := contentWidth(termW)
	rows := c.rows()
	c.pageSize = max(1, termH-checklistChrome)
	c.scrollToCursor(len(rows))

	var out screen
	out.line("")
	out.line(banner(c.title, width))
	out.line("")
	out.line("  " + c.searchLine(width))
	out.line("")
	c.writeRows(&out, rows, width)
	out.line("")
	out.line("  " + c.footerLine(width))
	out.line("  " + warning(clip(c.message, width)))
	return out.String()
}

// scrollToCursor keeps the cursor on screen.
func (c *Checklist) scrollToCursor(rowCount int) {
	c.cursor = min(max(c.cursor, 0), max(rowCount-1, 0))
	if c.cursor < c.scroll {
		c.scroll = c.cursor
	}
	if c.cursor >= c.scroll+c.pageSize {
		c.scroll = c.cursor - c.pageSize + 1
	}
	c.scroll = min(c.scroll, max(rowCount-c.pageSize, 0))
}

func (c *Checklist) searchLine(width int) string {
	text := muted(clip(c.placeholder, width-3))
	if len(c.search) > 0 {
		text = clip(string(c.search), width-3)
	}
	return accent("⌕") + " " + text + accent("▏")
}

func (c *Checklist) writeRows(out *screen, rows []checkRow, width int) {
	if len(rows) == 0 {
		out.line("    " + muted(clip("nothing matches", width-2)))
	}
	cols := c.columns(width)
	end := min(c.scroll+c.pageSize, len(rows))
	for i := c.scroll; i < end; i++ {
		out.line(c.renderRow(rows[i], i == c.cursor, cols))
	}
	if hidden := len(rows) - end; hidden > 0 {
		out.line("    " + muted(fmt.Sprintf("↓ %d more", hidden)))
	} else {
		out.line("")
	}
}

// footerLine is the status plus as many key hints as fit.
func (c *Checklist) footerLine(width int) string {
	status := c.summary(c)
	fits := func(hint string, gap int) bool {
		return width >= utf8.RuneCountInString(status)+utf8.RuneCountInString(hint)+gap
	}
	switch {
	case fits(c.hints[0], 3):
		return success(status) + "   " + muted(c.hints[0])
	case fits(c.hints[1], 2):
		return success(status) + "  " + muted(c.hints[1])
	default:
		return success(clip(status, width))
	}
}

// ---------- rows ----------

// checklistColumns are the widths of each part of a row; 0 hides a column.
type checklistColumns struct {
	labelW, detailW, columnW, metaW int
}

// columns fits the row parts into width. Optional columns are dropped on
// narrow consoles; descriptions get the space left after the label.
func (c *Checklist) columns(width int) checklistColumns {
	var cols checklistColumns
	hasDetail, hasColumn, hasMeta := false, false, c.trackChanges
	for _, it := range c.items {
		hasDetail = hasDetail || it.detail != ""
		hasColumn = hasColumn || it.column != ""
		hasMeta = hasMeta || it.meta != ""
	}

	rest := width - 4 // pointer, check mark, gaps
	if hasColumn && width >= 44 {
		cols.columnW = appIDWidth
		rest -= cols.columnW + 2
	}
	if hasMeta && width >= 56 {
		cols.metaW = 9
		rest -= cols.metaW + 2
	}
	cols.labelW = max(4, rest)
	if hasDetail && rest >= 40 {
		cols.labelW = min(30, rest*2/5)
		cols.detailW = rest - cols.labelW - 2
	}
	return cols
}

func (c *Checklist) renderRow(row checkRow, atCursor bool, cols checklistColumns) string {
	pointer := "  "
	if atCursor {
		pointer = accent("›") + " "
	}
	if row.add != nil {
		text := fit(row.addText, cols.labelW)
		if atCursor {
			text = bold(text)
		}
		return "  " + pointer + accent("+") + " " + text
	}

	it := row.item
	label := fit(it.label, cols.labelW)
	switch {
	case atCursor:
		label = bold(label)
	case it.faded:
		label = muted(label)
	}

	out := "  " + pointer + c.checkMark(it) + " " + label
	if cols.detailW > 0 {
		out += "  " + muted(fit(it.detail, cols.detailW))
	}
	if cols.columnW > 0 {
		out += "  " + muted(fit(it.column, cols.columnW))
	}
	if cols.metaW > 0 {
		out += "  " + c.metaText(it, cols.metaW)
	}
	return out
}

// checkMark is ◉ when checked, ○ when not; red ○ for a pending uncheck.
func (c *Checklist) checkMark(it *checkItem) string {
	switch {
	case it.checked:
		return success("◉")
	case c.trackChanges && it.initial:
		return danger("○")
	default:
		return muted("○")
	}
}

// metaText is the right column: the pending change, or the item's meta text.
func (c *Checklist) metaText(it *checkItem, w int) string {
	if !c.trackChanges {
		return muted(fmt.Sprintf("%*s", w, clip(it.meta, w)))
	}
	switch {
	case it.checked && !it.initial:
		return success(fit(c.changeLabels[0], w))
	case !it.checked && it.initial:
		return danger(fit(c.changeLabels[1], w))
	case it.checked:
		return muted(fit(c.changeLabels[2], w))
	default:
		return fit("", w)
	}
}
