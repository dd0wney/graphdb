package compliance

import (
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// ExportReport exports the compliance report in various formats
func (c *ComplianceChecker) ExportReport(report *ComplianceReport, format string, writer io.Writer) error {
	switch format {
	case "json":
		return c.exportJSON(report, writer)
	case "text":
		return c.exportText(report, writer)
	case "markdown":
		return c.exportMarkdown(report, writer)
	default:
		return fmt.Errorf("unsupported format: %s", format)
	}
}

// exportJSON exports report as JSON
func (c *ComplianceChecker) exportJSON(report *ComplianceReport, writer io.Writer) error {
	// Note: json.NewEncoder does not return an error - it always succeeds
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}

// exportText exports report as plain text
func (c *ComplianceChecker) exportText(report *ComplianceReport, writer io.Writer) error {
	fmt.Fprintf(writer, "Compliance Report: %s\n", report.Framework)
	fmt.Fprintf(writer, "Generated: %s\n", report.GeneratedAt.Format(time.RFC3339))
	fmt.Fprintf(writer, "Version: %s\n\n", report.Version)

	fmt.Fprintf(writer, "Summary:\n")
	fmt.Fprintf(writer, "  Total Controls: %d\n", report.Summary.TotalControls)
	fmt.Fprintf(writer, "  Compliant: %d\n", report.Summary.CompliantControls)
	fmt.Fprintf(writer, "  Partial: %d\n", report.Summary.PartialControls)
	fmt.Fprintf(writer, "  Non-Compliant: %d\n", report.Summary.NonCompliantControls)
	fmt.Fprintf(writer, "  Compliance Score: %.1f%%\n\n", report.Summary.ComplianceScore)

	fmt.Fprintf(writer, "Controls:\n")
	for _, control := range report.Controls {
		fmt.Fprintf(writer, "\n[%s] %s - %s\n", control.Status, control.ID, control.Title)
		if len(control.Evidence) > 0 {
			fmt.Fprintf(writer, "  Evidence: %d item(s)\n", len(control.Evidence))
		}
		if control.Notes != "" {
			fmt.Fprintf(writer, "  Notes: %s\n", control.Notes)
		}
	}

	return nil
}

// exportMarkdown exports report as Markdown
func (c *ComplianceChecker) exportMarkdown(report *ComplianceReport, writer io.Writer) error {
	fmt.Fprintf(writer, "# %s Compliance Report\n\n", report.Framework)
	fmt.Fprintf(writer, "**Generated:** %s\n\n", report.GeneratedAt.Format("2006-01-02 15:04:05"))

	fmt.Fprintf(writer, "## Summary\n\n")
	fmt.Fprintf(writer, "| Metric | Value |\n")
	fmt.Fprintf(writer, "|--------|-------|\n")
	fmt.Fprintf(writer, "| Total Controls | %d |\n", report.Summary.TotalControls)
	fmt.Fprintf(writer, "| Compliant | %d |\n", report.Summary.CompliantControls)
	fmt.Fprintf(writer, "| Partial | %d |\n", report.Summary.PartialControls)
	fmt.Fprintf(writer, "| Non-Compliant | %d |\n", report.Summary.NonCompliantControls)
	fmt.Fprintf(writer, "| **Compliance Score** | **%.1f%%** |\n\n", report.Summary.ComplianceScore)

	fmt.Fprintf(writer, "## Controls\n\n")
	for _, control := range report.Controls {
		statusEmoji := getStatusEmoji(control.Status)
		fmt.Fprintf(writer, "### %s %s: %s\n\n", statusEmoji, control.ID, control.Title)
		fmt.Fprintf(writer, "**Status:** %s\n\n", control.Status)
		fmt.Fprintf(writer, "%s\n\n", control.Description)

		if len(control.Evidence) > 0 {
			fmt.Fprintf(writer, "**Evidence:**\n\n")
			for _, ev := range control.Evidence {
				fmt.Fprintf(writer, "- %s: %s (Source: %s)\n", ev.Type, ev.Description, ev.Source)
			}
			fmt.Fprintf(writer, "\n")
		}

		if control.Notes != "" {
			fmt.Fprintf(writer, "**Notes:** %s\n\n", control.Notes)
		}

		fmt.Fprintf(writer, "---\n\n")
	}

	return nil
}
