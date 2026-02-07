package ui

import "strings"

type WorktreeRow struct {
	BranchLabel     string
	PRLabel         string
	CILabel         string
	ReviewLabel     string
	CommentsLabel   string
	UnresolvedLabel string
	PRStatusLabel   string
	Disabled        bool
}

func RenderWorktreeSelector(rows []WorktreeRow, cursor int, styles Styles) string {
	const (
		branchWidth     = 40
		prWidth         = 12
		ciWidth         = 24
		approvalWidth   = 12
		commentsWidth   = 10
		unresolvedWidth = 10
		prStateWidth    = 17
	)
	var b strings.Builder
	header := formatWorktreeLine("Branch", "PR", "CI", "Approval", "Comments", "Unresolved", "PR Status", branchWidth, prWidth, ciWidth, approvalWidth, commentsWidth, unresolvedWidth, prStateWidth)
	b.WriteString(styles.Header("  " + header))
	b.WriteString("\n")
	for i, row := range rows {
		rowStyle := styles.Normal
		rowSelectedStyle := styles.Selected
		if row.Disabled {
			rowStyle = styles.Disabled
			rowSelectedStyle = styles.DisabledSelected
		}
		line := formatWorktreeLine(
			row.BranchLabel,
			row.PRLabel,
			row.CILabel,
			row.ReviewLabel,
			row.CommentsLabel,
			row.UnresolvedLabel,
			row.PRStatusLabel,
			branchWidth,
			prWidth,
			ciWidth,
			approvalWidth,
			commentsWidth,
			unresolvedWidth,
			prStateWidth,
		)
		if i == cursor {
			b.WriteString("  " + rowSelectedStyle(line))
		} else {
			b.WriteString("  " + rowStyle(line))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func formatWorktreeLine(branch string, pr string, ci string, approval string, comments string, unresolved string, prState string, branchWidth int, prWidth int, ciWidth int, approvalWidth int, commentsWidth int, unresolvedWidth int, prStateWidth int) string {
	return PadOrTrim(branch, branchWidth) + " " +
		PadOrTrim(pr, prWidth) + " " +
		PadOrTrim(ci, ciWidth) + " " +
		PadOrTrim(approval, approvalWidth) + " " +
		PadOrTrim(comments, commentsWidth) + " " +
		PadOrTrim(unresolved, unresolvedWidth) + " " +
		PadOrTrim(prState, prStateWidth)
}
