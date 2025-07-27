package github

// Review represents a PR review
type Review struct {
	State string
	User  string
	Body  string
}

// CheckStatus represents the combined CI check status
type CheckStatus struct {
	State       string // success, failure, pending, error
	Description string
	Details     []CheckDetail
}

// CheckDetail represents a single CI check
type CheckDetail struct {
	Name        string
	Status      string
	Description string
	URL         string
}

// DiffStats represents PR diff statistics
type DiffStats struct {
	Additions int
	Deletions int
	Changes   int
	Files     int
}