package ui

import (
	"fmt"
	"strings"
)

type PRRow struct {
	NumberLabel     string
	Branch          string
	Title           string
	CILabel         string
	ApprovalLabel   string
	CommentsLabel   string
	UnresolvedLabel string
	StatusLabel     string
	Inactive        bool
}

func RenderPRSelector(rows []PRRow, cursor int, loading bool, loadingGlyph string, styles Styles) string {
	const (
		numberWidth     = 8
		branchWidth     = 32
		titleWidth      = 68
		ciWidth         = 24
		approvalWidth   = 14
		commentsWidth   = 10
		unresolvedWidth = 10
		statusWidth     = 17
	)
	var b strings.Builder
	header := formatPRLine("PR", "Branch", "Title", "CI", "Approval", "Comments", "Unresolved", "PR Status", numberWidth, branchWidth, titleWidth, ciWidth, approvalWidth, commentsWidth, unresolvedWidth, statusWidth)
	b.WriteString(styles.Header("  " + header))
	b.WriteString("\n")
	if len(rows) == 0 {
		b.WriteString("  ")
		b.WriteString(styles.Disabled("No PRs."))
		if loading {
			b.WriteString("\n  ")
			b.WriteString(styles.Secondary(loadingGlyph + " Loading PRs..."))
		}
		return b.String()
	}
	for i, row := range rows {
		rowStyle := styles.Normal
		rowSelectedStyle := styles.Selected
		if row.Inactive {
			rowStyle = styles.Disabled
			rowSelectedStyle = styles.DisabledSelected
		}
		line := formatPRLine(
			row.NumberLabel,
			row.Branch,
			row.Title,
			row.CILabel,
			row.ApprovalLabel,
			row.CommentsLabel,
			row.UnresolvedLabel,
			row.StatusLabel,
			numberWidth,
			branchWidth,
			titleWidth,
			ciWidth,
			approvalWidth,
			commentsWidth,
			unresolvedWidth,
			statusWidth,
		)
		if i == cursor {
			b.WriteString("  " + rowSelectedStyle(line))
		} else {
			b.WriteString("  " + rowStyle(line))
		}
		b.WriteString("\n")
	}
	if loading {
		b.WriteString("  ")
		b.WriteString(styles.Secondary(loadingGlyph + " Loading PRs..."))
	}
	return b.String()
}

func formatPRLine(number string, branch string, title string, ci string, approval string, comments string, unresolved string, status string, numberWidth int, branchWidth int, titleWidth int, ciWidth int, approvalWidth int, commentsWidth int, unresolvedWidth int, statusWidth int) string {
	return PadOrTrim(number, numberWidth) + " " +
		PadOrTrim(branch, branchWidth) + " " +
		PadOrTrim(title, titleWidth) + " " +
		PadOrTrim(ci, ciWidth) + " " +
		PadOrTrim(approval, approvalWidth) + " " +
		PadOrTrim(comments, commentsWidth) + " " +
		PadOrTrim(unresolved, unresolvedWidth) + " " +
		PadOrTrim(status, statusWidth)
}

func BuildPRRow(number int, branch string, title string, ciDone int, ciTotal int, ciState string, ciFailingNames string, reviewApproved int, reviewRequired int, reviewKnown bool, unresolvedComments int, resolvedComments int, commentThreadsTotal int, commentsKnown bool, status string) PRRow {
	return PRRow{
		NumberLabel:     fmt.Sprintf("#%d", number),
		Branch:          branch,
		Title:           title,
		CILabel:         formatPRListCI(ciDone, ciTotal, ciState, ciFailingNames),
		ApprovalLabel:   formatPRListApproval(reviewApproved, reviewRequired, reviewKnown),
		CommentsLabel:   formatCommentsLabel(resolvedComments, commentThreadsTotal, commentsKnown),
		UnresolvedLabel: formatUnresolvedLabel(unresolvedComments, commentsKnown),
		StatusLabel:     formatPRListStatus(status),
		Inactive:        isInactivePRStatus(status),
	}
}

func formatPRListCI(ciDone int, ciTotal int, ciState string, ciFailingNames string) string {
	if ciTotal == 0 {
		return "-"
	}
	switch strings.TrimSpace(ciState) {
	case "success":
		return fmt.Sprintf("✓ %d/%d", ciDone, ciTotal)
	case "fail":
		names := strings.TrimSpace(ciFailingNames)
		if names != "" {
			return fmt.Sprintf("✗ %d/%d %s", ciDone, ciTotal, names)
		}
		return fmt.Sprintf("✗ %d/%d", ciDone, ciTotal)
	case "in_progress":
		return fmt.Sprintf("… %d/%d", ciDone, ciTotal)
	default:
		return "-"
	}
}

func formatCommentsLabel(resolved int, total int, known bool) string {
	if !known || total <= 0 {
		return "-"
	}
	if resolved < 0 {
		resolved = 0
	}
	if resolved > total {
		resolved = total
	}
	return fmt.Sprintf("(%d/%d)", resolved, total)
}

func formatUnresolvedLabel(unresolved int, known bool) string {
	if !known {
		return "-"
	}
	if unresolved < 0 {
		unresolved = 0
	}
	return fmt.Sprintf("%d", unresolved)
}

func formatPRListApproval(reviewApproved int, reviewRequired int, reviewKnown bool) string {
	if reviewRequired > 0 {
		return fmt.Sprintf("%d/%d", reviewApproved, reviewRequired)
	}
	if reviewKnown && reviewApproved > 0 {
		return "1/1"
	}
	return "-"
}

func formatPRListStatus(status string) string {
	s := strings.TrimSpace(strings.ToLower(status))
	if s == "" {
		return "-"
	}
	return s
}

func isInactivePRStatus(status string) bool {
	s := strings.TrimSpace(strings.ToLower(status))
	return s == "closed" || s == "merged"
}
