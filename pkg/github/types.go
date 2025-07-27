package github

import "log/slog"

// Review represents a PR review
type Review struct {
	State string
	User  string
	Body  string
}

// LogValue implements slog.LogValuer for structured logging
func (r *Review) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("state", r.State),
		slog.String("user", r.User),
		slog.String("body_preview", truncateString(r.Body, 50)),
	)
}

// CheckStatus represents the combined CI check status
type CheckStatus struct {
	State       string // success, failure, pending, error
	Description string
	Details     []CheckDetail
}

// LogValue implements slog.LogValuer for structured logging
func (cs *CheckStatus) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("state", cs.State),
		slog.String("description", cs.Description),
		slog.Int("check_count", len(cs.Details)),
	)
}

// CheckDetail represents a single CI check
type CheckDetail struct {
	Name        string
	Status      string
	Description string
	URL         string
}

// LogValue implements slog.LogValuer for structured logging
func (cd *CheckDetail) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("name", cd.Name),
		slog.String("status", cd.Status),
		slog.String("description", truncateString(cd.Description, 50)),
	)
}

// DiffStats represents PR diff statistics
type DiffStats struct {
	Additions int
	Deletions int
	Changes   int
	Files     int
}

// LogValue implements slog.LogValuer for structured logging
func (ds *DiffStats) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Int("additions", ds.Additions),
		slog.Int("deletions", ds.Deletions),
		slog.Int("files", ds.Files),
		slog.Int("total_changes", ds.Additions+ds.Deletions),
	)
}

// truncateString truncates a string to maxLen characters
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}