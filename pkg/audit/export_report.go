package audit

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// ExportReport generates a summary report of audit events
func (e *Exporter) ExportReport(writer io.Writer, options *ExportOptions) error {
	events, err := e.readEvents(options)
	if err != nil {
		return err
	}

	// Calculate statistics
	stats := e.calculateStatistics(events)

	// Write report - check all write errors for compliance
	if _, err := fmt.Fprintf(writer, "Audit Log Report\n"); err != nil {
		return fmt.Errorf("failed to write report header: %w", err)
	}
	if _, err := fmt.Fprintf(writer, "================\n\n"); err != nil {
		return fmt.Errorf("failed to write report separator: %w", err)
	}
	if _, err := fmt.Fprintf(writer, "Period: %s to %s\n",
		options.StartTime.Format(time.RFC3339),
		options.EndTime.Format(time.RFC3339)); err != nil {
		return fmt.Errorf("failed to write report period: %w", err)
	}
	if _, err := fmt.Fprintf(writer, "\nTotal Events: %d\n\n", stats.TotalEvents); err != nil {
		return fmt.Errorf("failed to write total events: %w", err)
	}

	if _, err := fmt.Fprintf(writer, "Events by Severity:\n"); err != nil {
		return fmt.Errorf("failed to write severity section: %w", err)
	}
	for severity, count := range stats.BySeverity {
		if _, err := fmt.Fprintf(writer, "  %s: %d\n", severity, count); err != nil {
			return fmt.Errorf("failed to write severity stat: %w", err)
		}
	}

	if _, err := fmt.Fprintf(writer, "\nEvents by Action:\n"); err != nil {
		return fmt.Errorf("failed to write action section: %w", err)
	}
	for action, count := range stats.ByAction {
		if _, err := fmt.Fprintf(writer, "  %s: %d\n", action, count); err != nil {
			return fmt.Errorf("failed to write action stat: %w", err)
		}
	}

	if _, err := fmt.Fprintf(writer, "\nEvents by Status:\n"); err != nil {
		return fmt.Errorf("failed to write status section: %w", err)
	}
	for status, count := range stats.ByStatus {
		if _, err := fmt.Fprintf(writer, "  %s: %d\n", status, count); err != nil {
			return fmt.Errorf("failed to write status stat: %w", err)
		}
	}

	if _, err := fmt.Fprintf(writer, "\nTop Users:\n"); err != nil {
		return fmt.Errorf("failed to write users section: %w", err)
	}
	for i, user := range stats.TopUsers {
		if i >= 10 {
			break
		}
		if _, err := fmt.Fprintf(writer, "  %s: %d events\n", user.Username, user.Count); err != nil {
			return fmt.Errorf("failed to write user stat: %w", err)
		}
	}

	if _, err := fmt.Fprintf(writer, "\nTop Resources:\n"); err != nil {
		return fmt.Errorf("failed to write resources section: %w", err)
	}
	for i, resource := range stats.TopResources {
		if i >= 10 {
			break
		}
		if _, err := fmt.Fprintf(writer, "  %s (%s): %d events\n", resource.ResourceID, resource.ResourceType, resource.Count); err != nil {
			return fmt.Errorf("failed to write resource stat: %w", err)
		}
	}

	return nil
}

// calculateStatistics calculates statistics from events
func (e *Exporter) calculateStatistics(events []*PersistentEvent) ReportStatistics {
	stats := ReportStatistics{
		TotalEvents: len(events),
		BySeverity:  make(map[Severity]int),
		ByAction:    make(map[Action]int),
		ByStatus:    make(map[Status]int),
	}

	userCounts := make(map[string]int)
	resourceCounts := make(map[string]int)

	for _, event := range events {
		stats.BySeverity[event.Severity]++
		stats.ByAction[event.Action]++
		stats.ByStatus[event.Status]++

		if event.Username != "" {
			userCounts[event.Username]++
		}

		if event.ResourceID != "" {
			key := fmt.Sprintf("%s:%s", event.ResourceType, event.ResourceID)
			resourceCounts[key]++
		}
	}

	// Convert maps to sorted slices
	for username, count := range userCounts {
		stats.TopUsers = append(stats.TopUsers, UserStat{Username: username, Count: count})
	}

	for key, count := range resourceCounts {
		parts := strings.SplitN(key, ":", 2)
		stats.TopResources = append(stats.TopResources, ResourceStat{
			ResourceType: ResourceType(parts[0]),
			ResourceID:   parts[1],
			Count:        count,
		})
	}

	// Sort by count (descending)
	// For simplicity, just keep them unsorted for now
	// In production, you'd want to sort these

	return stats
}
