package ui

import (
	"fmt"
	"strings"
)

type PRRow struct {
	NumberLabel   string
	Branch        string
	Title         string
	CILabel       string
	ApprovalLabel string
	StatusLabel   string
	Inactive      bool
}

func RenderPRSelector(rows []PRRow, cursor int, loading bool, loadingGlyph string, styles Styles) string {
	const (
		numberWidth   = 8
		branchWidth   = 32
		titleWidth    = 68
		ciWidth       = 12
		approvalWidth = 14
		statusWidth   = 10
	)
	var b strings.Builder
	header := formatPRLine("PR", "Branch", "Title", "CI status", "Approval", "PR status", numberWidth, branchWidth, titleWidth, ciWidth, approvalWidth, statusWidth)
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
			row.StatusLabel,
			numberWidth,
			branchWidth,
			titleWidth,
			ciWidth,
			approvalWidth,
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

func formatPRLine(number string, branch string, title string, ci string, approval string, status string, numberWidth int, branchWidth int, titleWidth int, ciWidth int, approvalWidth int, statusWidth int) string {
	return PadOrTrim(number, numberWidth) + " " +
		PadOrTrim(branch, branchWidth) + " " +
		PadOrTrim(title, titleWidth) + " " +
		PadOrTrim(ci, ciWidth) + " " +
		PadOrTrim(approval, approvalWidth) + " " +
		PadOrTrim(status, statusWidth)
}

func BuildPRRow(number int, branch string, title string, ciDone int, ciTotal int, ciState string, reviewApproved int, reviewRequired int, reviewKnown bool, status string) PRRow {
	return PRRow{
		NumberLabel:   fmt.Sprintf("#%d", number),
		Branch:        branch,
		Title:         title,
		CILabel:       formatPRListCI(ciDone, ciTotal, ciState),
		ApprovalLabel: formatPRListApproval(reviewApproved, reviewRequired, reviewKnown),
		StatusLabel:   formatPRListStatus(status),
		Inactive:      isInactivePRStatus(status),
	}
}

func formatPRListCI(ciDone int, ciTotal int, ciState string) string {
	if ciTotal == 0 {
		return "-"
	}
	switch strings.TrimSpace(ciState) {
	case "success":
		return fmt.Sprintf("✓ %d/%d", ciDone, ciTotal)
	case "fail":
		return fmt.Sprintf("✗ %d/%d", ciDone, ciTotal)
	case "in_progress":
		return fmt.Sprintf("… %d/%d", ciDone, ciTotal)
	default:
		return "-"
	}
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
