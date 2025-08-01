package ui

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/kennyp/speedrun/pkg/agent"
	"github.com/kennyp/speedrun/pkg/config"
	"github.com/kennyp/speedrun/pkg/github"
)

// Styles
var (
	titleStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#7D56F4")).
			Foreground(lipgloss.Color("#FAFAFA")).
			Padding(0, 1)

	statusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FAFAFA")).
			Background(lipgloss.Color("#7D56F4")).
			Padding(0, 1)

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#626262"))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF0000"))

	successStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#00FF00"))
)

// Global atomic counter for generating unique PR IDs
var nextPRID atomic.Int64

// PR type detection keywords
var (
	dependencyKeywords    = []string{"bump ", "update ", "upgrade ", "dependencies", "snyk", "dependabot"}
	documentationKeywords = []string{"readme", "doc", "documentation", "guide", "rfc"}
)

// Helper functions for atomic ID-based lookups

// findPRByID finds a PR item by its atomic ID
func (m Model) findPRByID(id int64) *PRItem {
	for i := range m.items {
		if m.items[i].ID == id {
			return &m.items[i]
		}
	}
	return nil
}

// updatePRByID updates a PR item by its atomic ID using the provided update function
func (m Model) updatePRByID(id int64, updateFunc func(*PRItem)) Model {
	for i := range m.items {
		if m.items[i].ID == id {
			updateFunc(&m.items[i])
			break
		}
	}
	return m
}

// triggerAIAnalysisIfReadyByID triggers AI analysis for a PR item by ID if all conditions are met
func (m Model) triggerAIAnalysisIfReadyByID(id int64) tea.Cmd {
	item := m.findPRByID(id)
	if item == nil {
		slog.Debug("PR not found for AI analysis trigger", slog.Int64("prID", id))
		return nil
	}

	// Convert back to index for the existing function
	for i := range m.items {
		if m.items[i].ID == id {
			return m.triggerAIAnalysisIfReady(i)
		}
	}
	return nil
}

// Model represents the TUI application state
type Model struct {
	ctx      context.Context
	config   *config.Config
	github   *github.Client
	aiAgent  *agent.Agent
	username string

	list     list.Model
	items    []PRItem
	status   string
	quitting bool
	spinner  spinner.Model
	help     help.Model

	// Loading states
	loadingPRs bool

	// Filter state
	showOnlyUnreviewed bool

	// Key bindings
	keys KeyMap

	// Popup state
	showPopup      bool
	popupContent   string
	popupScrollPos int // Current scroll position in popup

	// Advanced filter dialog state
	showAdvancedFilter bool
	filterReviewStatus string // "all", "reviewed", "unreviewed"
	filterRepo         string
	filterType         string // "all", "docs", "code", "dependencies", "mixed"
}

// KeyMap defines key bindings for speedrun-specific actions
type KeyMap struct {
	Approve        key.Binding
	View           key.Binding
	AutoMerge      key.Binding
	Filter         key.Binding
	FilterAdvanced key.Binding
	Details        key.Binding
	Help           key.Binding
	Quit           key.Binding
	Refresh        key.Binding
}

// DefaultKeyMap returns the default key bindings
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Approve: key.NewBinding(
			key.WithKeys("a"),
			key.WithHelp("a", "approve"),
		),
		View: key.NewBinding(
			key.WithKeys("v"),
			key.WithHelp("v", "view in browser"),
		),
		AutoMerge: key.NewBinding(
			key.WithKeys("m"),
			key.WithHelp("m", "auto-merge"),
		),
		Filter: key.NewBinding(
			key.WithKeys("f"),
			key.WithHelp("f", "filter reviewed"),
		),
		FilterAdvanced: key.NewBinding(
			key.WithKeys("F"),
			key.WithHelp("F", "advanced filter"),
		),
		Details: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "show details"),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "show help"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", "quit"),
		),
		Refresh: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "refresh"),
		),
	}
}

// CombinedKeyMap combines list navigation keys with speedrun-specific keys
type CombinedKeyMap struct {
	ListKeys     list.KeyMap // The list's KeyMap for navigation
	SpeedrunKeys KeyMap      // Our custom KeyMap for actions
}

// ShortHelp returns keybindings to be shown in the mini help view. It's part
// of the key.Map interface.
func (k CombinedKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{
		k.ListKeys.CursorUp, k.ListKeys.CursorDown,
		k.SpeedrunKeys.Approve, k.SpeedrunKeys.View, k.SpeedrunKeys.AutoMerge, k.SpeedrunKeys.Details,
		k.SpeedrunKeys.Filter, k.SpeedrunKeys.Help, k.SpeedrunKeys.Quit,
	}
}

// FullHelp returns keybindings for the expanded help view. It's part of the
// key.Map interface.
func (k CombinedKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.ListKeys.CursorUp, k.ListKeys.CursorDown, k.ListKeys.PrevPage, k.ListKeys.NextPage},          // Navigation
		{k.ListKeys.GoToStart, k.ListKeys.GoToEnd},                                                      // Navigation (jump)
		{k.SpeedrunKeys.Approve, k.SpeedrunKeys.View, k.SpeedrunKeys.AutoMerge, k.SpeedrunKeys.Details}, // Actions
		{k.SpeedrunKeys.Filter, k.SpeedrunKeys.FilterAdvanced, k.SpeedrunKeys.Refresh},                  // Filtering & Refresh
		{k.SpeedrunKeys.Help, k.SpeedrunKeys.Quit},                                                      // Other
	}
}

// NewModel creates a new TUI model
func NewModel(ctx context.Context, cfg *config.Config, githubClient *github.Client, aiAgent *agent.Agent, username string) Model {
	// Create list
	l := list.New([]list.Item{}, list.NewDefaultDelegate(), 0, 0)
	l.Title = fmt.Sprintf("🔍 Pull Requests for %s", username)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.SetShowHelp(false) // Disable built-in help to prevent ? key conflicts
	l.Styles.Title = titleStyle

	// Create spinner
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	// Create help model
	h := help.New()
	h.ShowAll = false // Start with short help

	// Create combined key map
	speedrunKeys := DefaultKeyMap()

	return Model{
		ctx:                ctx,
		config:             cfg,
		github:             githubClient,
		aiAgent:            aiAgent,
		username:           username,
		list:               l,
		items:              []PRItem{},
		status:             "Loading pull requests...",
		spinner:            s,
		help:               h,
		keys:               speedrunKeys,
		loadingPRs:         true,
		showOnlyUnreviewed: true,         // Default to showing only unreviewed PRs
		filterReviewStatus: "unreviewed", // Default filter
		filterType:         "all",
		filterRepo:         "all",
	}
}

// Init initializes the model
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		FetchPRsCmd(m.github),
	)
}

// Update handles messages
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.list.SetWidth(msg.Width)
		m.list.SetHeight(msg.Height - 4) // Reserve space for status and help
		return m, nil

	case tea.KeyMsg:
		// Handle advanced filter dialog keys first
		if m.showAdvancedFilter {
			slog.Debug("Advanced filter dialog key pressed", slog.String("key", msg.String()))
			switch {
			case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
				slog.Info("User cancelled advanced filter dialog")
				m.showAdvancedFilter = false
				return m, nil
			case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
				slog.Info("User applied advanced filters", slog.String("review_status", m.filterReviewStatus), slog.String("type", m.filterType), slog.String("repo", m.filterRepo))
				// Apply filters and close dialog
				m.showAdvancedFilter = false
				m = m.applyAdvancedFilters()
				return m, nil
			// Review Status options
			case key.Matches(msg, key.NewBinding(key.WithKeys("1"))):
				slog.Debug("Advanced filter: review status changed to all")
				m.filterReviewStatus = "all"
				return m, nil
			case key.Matches(msg, key.NewBinding(key.WithKeys("2"))):
				slog.Debug("Advanced filter: review status changed to unreviewed")
				m.filterReviewStatus = "unreviewed"
				return m, nil
			case key.Matches(msg, key.NewBinding(key.WithKeys("3"))):
				slog.Debug("Advanced filter: review status changed to reviewed")
				m.filterReviewStatus = "reviewed"
				return m, nil
			// PR Type options
			case key.Matches(msg, key.NewBinding(key.WithKeys("4"))):
				slog.Debug("Advanced filter: PR type changed to all")
				m.filterType = "all"
				return m, nil
			case key.Matches(msg, key.NewBinding(key.WithKeys("5"))):
				slog.Debug("Advanced filter: PR type changed to docs")
				m.filterType = "docs"
				return m, nil
			case key.Matches(msg, key.NewBinding(key.WithKeys("6"))):
				slog.Debug("Advanced filter: PR type changed to code")
				m.filterType = "code"
				return m, nil
			case key.Matches(msg, key.NewBinding(key.WithKeys("7"))):
				slog.Debug("Advanced filter: PR type changed to dependencies")
				m.filterType = "dependencies"
				return m, nil
			case key.Matches(msg, key.NewBinding(key.WithKeys("8"))):
				slog.Debug("Advanced filter: PR type changed to mixed")
				m.filterType = "mixed"
				return m, nil
			// Repository Filter options
			case key.Matches(msg, key.NewBinding(key.WithKeys("9"))):
				slog.Debug("Advanced filter: repo filter changed to all")
				m.filterRepo = "all"
				return m, nil
			case key.Matches(msg, key.NewBinding(key.WithKeys("0"))):
				slog.Debug("Advanced filter: repo filter changed to current")
				m.filterRepo = "current"
				return m, nil
			default:
				slog.Debug("Advanced filter: unhandled key", slog.String("key", msg.String()))
			}
			return m, nil // Consume all other keys when advanced filter dialog is open
		}

		// Handle popup-specific keys
		if m.showPopup {
			switch {
			case key.Matches(msg, m.keys.Details) || key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
				m.showPopup = false
				m.popupScrollPos = 0 // Reset scroll position
				slog.Debug("Popup closed by user")
				return m, nil
			case key.Matches(msg, key.NewBinding(key.WithKeys("up"), key.WithKeys("k"))):
				if m.popupScrollPos > 0 {
					m.popupScrollPos--
				}
				return m, nil
			case key.Matches(msg, key.NewBinding(key.WithKeys("down"), key.WithKeys("j"))):
				m.popupScrollPos++
				return m, nil
			case key.Matches(msg, key.NewBinding(key.WithKeys("pgup"))):
				m.popupScrollPos = max(0, m.popupScrollPos-10)
				return m, nil
			case key.Matches(msg, key.NewBinding(key.WithKeys("pgdown"))):
				m.popupScrollPos += 10
				return m, nil
			case key.Matches(msg, key.NewBinding(key.WithKeys("home"))):
				m.popupScrollPos = 0
				return m, nil
			case key.Matches(msg, key.NewBinding(key.WithKeys("end"))):
				// Will be handled in rendering to set to max scroll
				m.popupScrollPos = 999999
				return m, nil
			case key.Matches(msg, m.keys.Approve):
				// Handle approve from popup
				return m.handleApprove()
			case key.Matches(msg, m.keys.View):
				// Handle view from popup
				return m.handleView()
			case key.Matches(msg, m.keys.AutoMerge):
				// Handle auto-merge from popup
				return m.handleAutoMerge()
			}
			return m, nil // Consume all other keys when popup is open
		}

		// Allow navigation even when loading
		switch {
		case key.Matches(msg, m.keys.Quit):
			m.quitting = true
			return m, tea.Quit

		case key.Matches(msg, m.keys.Approve):
			return m.handleApprove()

		case key.Matches(msg, m.keys.View):
			return m.handleView()

		case key.Matches(msg, m.keys.Filter):
			return m.handleFilter()

		case key.Matches(msg, m.keys.FilterAdvanced):
			return m.handleFilterAdvanced()

		case key.Matches(msg, m.keys.Details):
			return m.handleDetails()

		case key.Matches(msg, m.keys.Help):
			return m.handleHelp()

		case key.Matches(msg, m.keys.Refresh):
			return m.handleRefresh()

		case key.Matches(msg, m.keys.AutoMerge):
			return m.handleAutoMerge()
		}

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case PRsLoadedMsg:
		return m.handlePRsLoaded(msg)

	case DiffStatsLoadedMsg:
		return m.handleDiffStatsLoaded(msg)

	case CheckStatusLoadedMsg:
		return m.handleCheckStatusLoaded(msg)

	case ReviewsLoadedMsg:
		return m.handleReviewsLoaded(msg)

	case AIAnalysisLoadedMsg:
		return m.handleAIAnalysisLoaded(msg)

	case TriggerAIAnalysisMsg:
		return m.handleTriggerAIAnalysis(msg)

	case SmartRefreshLoadedMsg:
		return m.handleSmartRefreshLoaded(msg)

	case PRApprovedMsg:
		return m.handlePRApproved(msg)

	case AutoMergeEnabledMsg:
		return m.handleAutoMergeEnabled(msg)

	case PRMergedMsg:
		return m.handlePRMerged(msg)

	case StatusMsg:
		m.status = string(msg)
		return m, nil
	}

	// Update list
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

// View renders the UI
func (m Model) View() string {
	if m.quitting {
		return "👋 Goodbye!\n"
	}

	// Show detailed info for selected PR
	details := ""
	if selected := m.list.SelectedItem(); selected != nil {
		if prItem, ok := selected.(PRItem); ok {
			details = m.renderPRDetails(prItem)
		}
	}

	// Help text
	var helpText string
	if m.showAdvancedFilter {
		helpText = helpStyle.Render("1-3: review • 4-8: type • 9-0: repo • enter: apply • esc: cancel")
	} else if m.showPopup {
		helpText = helpStyle.Render("a: approve • v: view • m: auto-merge • ↑/j: scroll • pgup/pgdown: page • enter/esc: close")
	} else {
		// Use the bubbles help system with combined keys
		m.help.Width = m.list.Width()
		combinedKeys := CombinedKeyMap{
			SpeedrunKeys: m.keys,
			ListKeys:     m.list.KeyMap,
		}
		helpText = m.help.View(combinedKeys)
	}

	// Status with spinner if loading
	status := m.status
	if m.loadingPRs {
		status = m.spinner.View() + " " + status
	}

	baseView := fmt.Sprintf(
		"%s%s\n%s\n%s",
		m.list.View(),
		details,
		statusStyle.Render(status),
		helpText,
	)

	// Overlay advanced filter dialog if shown
	if m.showAdvancedFilter {
		return m.renderAdvancedFilterDialog(baseView)
	}

	// Overlay popup if shown
	if m.showPopup {
		return m.renderPopup(baseView)
	}

	return baseView
}

// renderPRDetails renders detailed information about a PR
func (m Model) renderPRDetails(item PRItem) string {
	// Only show loading if there are actual loading operations
	stillLoading := item.LoadingDiff || item.LoadingChecks || item.LoadingReviews || item.LoadingAI
	if stillLoading {
		return "\n💭 Loading PR details..."
	}

	details := fmt.Sprintf("\n📍 %s/%s#%d", item.PR.Owner, item.PR.Repo, item.PR.Number)

	// Add more details as they become available
	if item.DiffStats != nil && item.CheckStatus != nil {
		details += fmt.Sprintf("\n💬 %d additions, %d deletions across %d files",
			item.DiffStats.Additions, item.DiffStats.Deletions, item.DiffStats.Files)
	}

	return details
}

// Message handlers

func (m Model) handlePRsLoaded(msg PRsLoadedMsg) (Model, tea.Cmd) {
	m.loadingPRs = false

	if msg.Err != nil {
		slog.Error("Failed to load PRs in UI", slog.Any("error", msg.Err))
		m.status = errorStyle.Render("Failed to load PRs: " + msg.Err.Error())
		return m, nil
	}

	slog.Info("PRs loaded in UI", slog.Int("pr_count", len(msg.PRs)),
		slog.Bool("show_only_unreviewed", m.showOnlyUnreviewed))

	// Create list items for all PRs (filtering will happen dynamically as review data loads)
	m.items = make([]PRItem, len(msg.PRs))

	for i, pr := range msg.PRs {
		// Check if AI analysis is already cached
		// Note: Skip cache check during startup since HeadSHA is not available yet
		// AI analysis will check cache properly when HeadSHA is populated
		loadingAI := m.aiAgent != nil

		m.items[i] = PRItem{
			ID:             nextPRID.Add(1),
			PR:             pr,
			LoadingDiff:    true,
			LoadingChecks:  true,
			LoadingReviews: true,
			LoadingAI:      loadingAI,
		}
	}

	// Apply initial filter (will show all PRs initially since review status is unknown)
	m = m.updateVisibleItems()

	// Update status message with filter information
	filterText := ""
	if m.showOnlyUnreviewed {
		filterText = " (unreviewed only)"
	}
	m.status = fmt.Sprintf("Found %d pull requests%s", len(msg.PRs), filterText)

	// Start loading details for each PR sequentially
	var sequences []tea.Cmd
	for i, pr := range msg.PRs {
		prID := m.items[i].ID
		// Create a sequence for each PR: diff → checks → reviews → AI
		prSequence := []tea.Cmd{
			FetchDiffStatsCmd(m.github, pr, prID),
			FetchCheckStatusCmd(m.github, pr, prID),
			FetchReviewsCmd(m.github, pr, m.username, prID),
		}

		// Add AI analysis to the sequence
		if !m.items[i].LoadingAI {
			// Load cached AI analysis immediately if available
			prSequence = append(prSequence, FetchCachedAIAnalysisCmd(pr, prID))
		}
		// Note: For LoadingAI=true, AI analysis will be triggered by the message handlers
		// when all prerequisites (diff, checks, reviews) are loaded

		// Add small delay between PR sequences to avoid overwhelming the API
		delay := time.Duration(i*100) * time.Millisecond
		if delay > 0 {
			sequences = append(sequences, tea.Tick(delay, func(t time.Time) tea.Msg {
				return tea.Sequence(prSequence...)()
			}))
		} else {
			sequences = append(sequences, tea.Sequence(prSequence...))
		}
	}

	return m, tea.Batch(sequences...)
}

func (m Model) handleDiffStatsLoaded(msg DiffStatsLoadedMsg) (Model, tea.Cmd) {
	m = m.updatePRByID(msg.PRID, func(item *PRItem) {
		item.LoadingDiff = false
		item.DiffStats = msg.Stats
		item.DiffError = msg.Err
	})

	// Re-apply filter to update the visible list
	m = m.updateVisibleItems()

	// Trigger AI analysis if we have all required data and AI agent is available
	return m, m.triggerAIAnalysisIfReadyByID(msg.PRID)
}

func (m Model) handleCheckStatusLoaded(msg CheckStatusLoadedMsg) (Model, tea.Cmd) {
	m = m.updatePRByID(msg.PRID, func(item *PRItem) {
		item.LoadingChecks = false
		item.CheckStatus = msg.Status
		item.CheckError = msg.Err
	})

	// Re-apply filter to update the visible list
	m = m.updateVisibleItems()

	// Trigger AI analysis if we have all required data and AI agent is available
	return m, m.triggerAIAnalysisIfReadyByID(msg.PRID)
}

func (m Model) handleReviewsLoaded(msg ReviewsLoadedMsg) (Model, tea.Cmd) {
	var prItem *PRItem
	m = m.updatePRByID(msg.PRID, func(item *PRItem) {
		prItem = item // Capture for logging
		item.LoadingReviews = false
		item.Reviews = msg.Reviews
		item.ReviewError = msg.Err

		// Check if current user has reviewed and determine review type
		userReviewed := false
		userApproved := false
		userDismissed := false
		for _, review := range msg.Reviews {
			if review.User == m.username {
				userReviewed = true
				switch review.State {
				case "APPROVED":
					userApproved = true
				case "DISMISSED":
					userDismissed = true
				}
				// Note: We don't break here because there might be multiple reviews
				// and we want to find the most recent status
			}
		}

		item.Reviewed = userReviewed
		item.Approved = userApproved
		item.Dismissed = userDismissed
	})

	if prItem != nil {
		slog.Debug("Reviews loaded for PR", slog.Any("pr", prItem.PR),
			slog.Int("total_reviews", len(msg.Reviews)), slog.Bool("user_reviewed", prItem.Reviewed),
			slog.Bool("user_approved", prItem.Approved), slog.Bool("user_dismissed", prItem.Dismissed), slog.Any("error", msg.Err))
	} else {
		slog.Debug("Reviews loaded for unknown PR", slog.Int64("prID", msg.PRID))
	}

	// Re-apply filter since review status may have changed
	m = m.updateVisibleItems()

	// Trigger AI analysis if we have all required data and AI agent is available
	return m, m.triggerAIAnalysisIfReadyByID(msg.PRID)
}

func (m Model) handleAIAnalysisLoaded(msg AIAnalysisLoadedMsg) (Model, tea.Cmd) {
	m = m.updatePRByID(msg.PRID, func(item *PRItem) {
		item.LoadingAI = false
		item.AIAnalysis = msg.Analysis
		item.AIError = msg.Err
	})

	// Re-apply filter to update the visible list
	m = m.updateVisibleItems()

	return m, nil
}

func (m Model) handleTriggerAIAnalysis(msg TriggerAIAnalysisMsg) (Model, tea.Cmd) {
	// Use the existing triggerAIAnalysisIfReadyByID method
	return m, m.triggerAIAnalysisIfReadyByID(msg.PRID)
}

func (m Model) handleSmartRefreshLoaded(msg SmartRefreshLoadedMsg) (Model, tea.Cmd) {
	m.loadingPRs = false

	if msg.Err != nil {
		m.status = errorStyle.Render("Failed to refresh PRs: " + msg.Err.Error())
		return m, nil
	}

	// Create maps for efficient lookups
	existingPRs := make(map[int]*PRItem)
	for i := range m.items {
		existingPRs[m.items[i].PR.Number] = &m.items[i]
	}

	freshPRMap := make(map[int]*github.PullRequest)
	for _, pr := range msg.PRs {
		freshPRMap[pr.Number] = pr
	}

	var newItems []PRItem
	newPRCount := 0
	updatedPRCount := 0

	// Process fresh PRs from GitHub
	for _, freshPR := range msg.PRs {
		if existingItem, exists := existingPRs[freshPR.Number]; exists {
			// Existing PR - check if it needs updates
			needsAIUpdate := false

			// Check if PR has new commits (HeadSHA changed)
			if existingItem.PR.HeadSHA != "" && freshPR.HeadSHA != "" &&
				existingItem.PR.HeadSHA != freshPR.HeadSHA {
				needsAIUpdate = true
				updatedPRCount++

				// Clear cached data for updated PR since commits changed
				// (but preserve reviews cache since those don't change with commits)
				existingItem.PR.InvalidateCommitRelatedCache()
			}

			// Update the PR data but preserve loading states and cached data
			updatedItem := *existingItem
			updatedItem.PR = freshPR // Update with fresh PR data

			// Reset loading states for data we want to refresh
			if needsAIUpdate {
				updatedItem.LoadingDiff = true
				updatedItem.LoadingChecks = true
				updatedItem.LoadingAI = m.aiAgent != nil
				updatedItem.DiffStats = nil
				updatedItem.CheckStatus = nil
				updatedItem.AIAnalysis = nil
			}
			// Reviews are already marked as loading from handleRefresh

			newItems = append(newItems, updatedItem)
		} else {
			// New PR - add with full loading state
			newPRCount++
			newItem := PRItem{
				ID:             nextPRID.Add(1),
				PR:             freshPR,
				LoadingDiff:    true,
				LoadingChecks:  true,
				LoadingReviews: true,
				LoadingAI:      m.aiAgent != nil,
			}
			newItems = append(newItems, newItem)
		}
	}

	// Update items list
	m.items = newItems

	// Apply filter to update visible items
	m = m.updateVisibleItems()

	// Update status with refresh results
	statusParts := []string{fmt.Sprintf("Refreshed %d PRs", len(msg.PRs))}
	if newPRCount > 0 {
		statusParts = append(statusParts, fmt.Sprintf("%d new", newPRCount))
	}
	if updatedPRCount > 0 {
		statusParts = append(statusParts, fmt.Sprintf("%d updated", updatedPRCount))
	}

	filterText := ""
	if m.showOnlyUnreviewed {
		filterText = " (unreviewed only)"
	}
	m.status = fmt.Sprintf("%s%s", strings.Join(statusParts, ", "), filterText)

	// Start loading data for new and updated PRs
	cmds := []tea.Cmd{}
	for i, item := range m.items {
		pr := item.PR
		prID := item.ID
		delay := time.Duration(i*50) * time.Millisecond

		// Load diff stats if needed
		if item.LoadingDiff {
			cmds = append(cmds, tea.Tick(delay, func(t time.Time) tea.Msg {
				return FetchDiffStatsCmd(m.github, pr, prID)()
			}))
		}

		// Load check status if needed
		if item.LoadingChecks {
			cmds = append(cmds, tea.Tick(delay+20*time.Millisecond, func(t time.Time) tea.Msg {
				return FetchCheckStatusCmd(m.github, pr, prID)()
			}))
		}

		// Always refresh reviews (user might have reviewed)
		if item.LoadingReviews {
			cmds = append(cmds, tea.Tick(delay+40*time.Millisecond, func(t time.Time) tea.Msg {
				return FetchReviewsCmd(m.github, pr, m.username, prID)()
			}))
		}
	}

	return m, tea.Batch(cmds...)
}

func (m Model) handlePRApproved(msg PRApprovedMsg) (Model, tea.Cmd) {
	if msg.Err != nil {
		slog.Error("PR approval failed in UI", slog.Int64("prID", msg.PRID), slog.Any("error", msg.Err))
		m.status = errorStyle.Render("Failed to approve PR: " + msg.Err.Error())
		return m, nil
	}

	var approvedPR *PRItem
	m = m.updatePRByID(msg.PRID, func(item *PRItem) {
		approvedPR = item // Capture for auto-merge logic
		item.Approved = true
		item.Reviewed = true
	})

	if approvedPR != nil {
		slog.Info("PR approved successfully in UI", slog.Any("pr", approvedPR.PR))
		m.status = successStyle.Render(fmt.Sprintf("✅ Approved PR #%d", approvedPR.PR.Number))
	}

	// Re-apply filter since review status changed
	m = m.updateVisibleItems()

	// Check if auto-merge should be triggered after approval
	nextCmd := m.moveToNext()
	if m.config.GitHub.AutoMergeOnApproval == "true" && approvedPR != nil {
		slog.Info("Auto-triggering auto-merge after approval", slog.Any("pr", approvedPR.PR))
		nextCmd = tea.Batch(m.moveToNext(), EnableAutoMergeCmd(approvedPR.PR, "SQUASH", approvedPR.ID))
	}

	return m, nextCmd
}

func (m Model) handleAutoMergeEnabled(msg AutoMergeEnabledMsg) (Model, tea.Cmd) {
	if msg.Err != nil {
		// Check if this is the specific "no failing checks" error that means we should merge directly
		errorMsg := msg.Err.Error()
		if strings.Contains(errorMsg, "pull request has no failing checks to resolve") {
			// GitHub says auto-merge isn't needed - the PR is ready for immediate merge
			item := m.findPRByID(msg.PRID)
			if item != nil {
				slog.Info("Auto-merge not needed, falling back to direct merge", slog.Any("pr", item.PR))
				m.status = fmt.Sprintf("PR #%d ready for immediate merge...", item.PR.Number)
				return m, MergeCmd(item.PR, "SQUASH", item.ID)
			}
		}

		// For any other auto-merge error, show the error to the user
		slog.Error("Auto-merge enabling failed in UI", slog.Int64("prID", msg.PRID), slog.Any("error", msg.Err))
		m.status = errorStyle.Render("Failed to enable auto-merge: " + msg.Err.Error())
		return m, nil
	}

	// Auto-merge enabled successfully
	item := m.findPRByID(msg.PRID)
	if item != nil {
		slog.Info("Auto-merge enabled successfully in UI", slog.Any("pr", item.PR))
		m.status = successStyle.Render(fmt.Sprintf("🔄 Auto-merge enabled for PR #%d", item.PR.Number))
	}

	return m, nil
}

func (m Model) handlePRMerged(msg PRMergedMsg) (Model, tea.Cmd) {
	if msg.Err != nil {
		slog.Error("PR merging failed in UI", slog.Int64("prID", msg.PRID), slog.Any("error", msg.Err))
		m.status = errorStyle.Render("Failed to merge PR: " + msg.Err.Error())
		return m, nil
	}

	// Find the PR item for status update
	item := m.findPRByID(msg.PRID)
	if item != nil {
		slog.Info("PR merged successfully in UI", slog.Any("pr", item.PR))
		m.status = successStyle.Render(fmt.Sprintf("✅ Merged PR #%d", item.PR.Number))
	}

	return m, nil
}

// Action handlers

func (m Model) handleApprove() (Model, tea.Cmd) {
	selected := m.list.SelectedItem()
	if selected == nil {
		slog.Debug("Approve action: no PR selected")
		return m, nil
	}

	prItem, ok := selected.(PRItem)
	if !ok {
		slog.Debug("Approve action: selected item is not a PR")
		return m, nil
	}

	if prItem.Approved {
		slog.Debug("Approve action: PR already approved", slog.Any("pr", prItem.PR))
		m.status = "PR already approved"
		return m, nil
	}

	slog.Info("User initiated PR approval", slog.Any("pr", prItem.PR),
		slog.Bool("reviewed", prItem.Reviewed), slog.Bool("approved", prItem.Approved))
	m.status = fmt.Sprintf("Approving PR #%d...", prItem.PR.Number)
	return m, ApprovePRCmd(prItem.PR, prItem.ID)
}

func (m Model) handleView() (Model, tea.Cmd) {
	selected := m.list.SelectedItem()
	if selected == nil {
		slog.Debug("View action: no PR selected")
		return m, nil
	}

	prItem, ok := selected.(PRItem)
	if !ok {
		slog.Debug("View action: selected item is not a PR")
		return m, nil
	}

	slog.Info("User opened PR in browser", slog.Any("pr", prItem.PR))
	return m, OpenPRInBrowserCmd(prItem.PR)
}

func (m Model) handleAutoMerge() (Model, tea.Cmd) {
	selected := m.list.SelectedItem()
	if selected == nil {
		slog.Debug("Auto-merge action: no PR selected")
		return m, nil
	}

	prItem, ok := selected.(PRItem)
	if !ok {
		slog.Debug("Auto-merge action: selected item is not a PR")
		return m, nil
	}

	slog.Info("User requested auto-merge", slog.Any("pr", prItem.PR))

	// Check auto-merge configuration
	switch m.config.GitHub.AutoMergeOnApproval {
	case "false":
		// Auto-merge disabled
		m.status = "Auto-merge is disabled in configuration"
		return m, nil
	case "true", "ask", "":
		// Always try auto-merge first - GitHub will tell us if it's not needed
		m.status = fmt.Sprintf("Enabling auto-merge for PR #%d...", prItem.PR.Number)
		return m, EnableAutoMergeCmd(prItem.PR, "SQUASH", prItem.ID)
	default:
		// Default to auto-merge attempt
		m.status = fmt.Sprintf("Enabling auto-merge for PR #%d...", prItem.PR.Number)
		return m, EnableAutoMergeCmd(prItem.PR, "SQUASH", prItem.ID)
	}
}

func (m Model) handleDetails() (Model, tea.Cmd) {
	selected := m.list.SelectedItem()
	if selected == nil {
		slog.Debug("Details action: no PR selected")
		return m, nil
	}

	prItem, ok := selected.(PRItem)
	if !ok {
		slog.Debug("Details action: selected item is not a PR")
		return m, nil
	}

	slog.Info("User opened PR details popup", slog.Any("pr", prItem.PR))
	m.showPopup = true
	m.popupScrollPos = 0 // Reset scroll position for new popup
	m.popupContent = m.generateDetailContent(prItem)
	return m, nil
}

func (m Model) handleRefresh() (Model, tea.Cmd) {
	slog.Info("User initiated refresh", slog.Int("current_items", len(m.items)),
		slog.Bool("show_only_unreviewed", m.showOnlyUnreviewed))

	m.loadingPRs = true
	m.status = "Checking for updates..."

	// Mark all existing reviews as loading to re-check review status
	for i := range m.items {
		m.items[i].LoadingReviews = true
	}

	// Re-apply filter to show loading state
	m = m.updateVisibleItems()

	return m, tea.Batch(
		m.spinner.Tick,
		SmartRefreshCmd(m.github),
	)
}

func (m Model) handleFilter() (Model, tea.Cmd) {
	// Check if advanced filters are active (non-default values)
	advancedFiltersActive := m.filterType != "all" || m.filterRepo != "all"

	slog.Info("User pressed f key",
		slog.Bool("advanced_filters_active", advancedFiltersActive),
		slog.String("current_review_status", m.filterReviewStatus),
		slog.String("current_type", m.filterType),
		slog.String("current_repo", m.filterRepo))

	if advancedFiltersActive {
		// When advanced filters are active, cycle through review status options
		oldReviewStatus := m.filterReviewStatus
		switch m.filterReviewStatus {
		case "all":
			m.filterReviewStatus = "unreviewed"
		case "unreviewed":
			m.filterReviewStatus = "reviewed"
		case "reviewed":
			m.filterReviewStatus = "all"
		}

		slog.Info("Advanced filter mode: cycled review status",
			slog.String("from", oldReviewStatus),
			slog.String("to", m.filterReviewStatus))

		// Update legacy flag for consistency
		m.showOnlyUnreviewed = (m.filterReviewStatus == "unreviewed")

		m.status = fmt.Sprintf("Review filter: %s (advanced filters active - use F to modify)", m.filterReviewStatus)
	} else {
		// Simple toggle when no advanced filters are active
		oldFilter := m.showOnlyUnreviewed
		m.showOnlyUnreviewed = !m.showOnlyUnreviewed

		// Update the filter status to match the toggle
		if m.showOnlyUnreviewed {
			m.filterReviewStatus = "unreviewed"
		} else {
			m.filterReviewStatus = "all"
		}

		slog.Info("Simple filter mode: toggled review filter",
			slog.Bool("from", oldFilter),
			slog.Bool("to", m.showOnlyUnreviewed))

		// Update status message
		filterStatus := "all"
		if m.showOnlyUnreviewed {
			filterStatus = "unreviewed only"
		}
		m.status = fmt.Sprintf("Filter toggled: showing %s PRs", filterStatus)
	}

	// Update visible items based on new filter state (don't preserve selection for user-initiated filter)
	m = m.updateVisibleItemsWithPreserveSelection(false)

	slog.Info("Filter applied",
		slog.String("review_filter", m.filterReviewStatus),
		slog.String("type_filter", m.filterType),
		slog.String("repo_filter", m.filterRepo),
		slog.Bool("advanced_active", advancedFiltersActive),
		slog.Int("visible_items", len(m.list.Items())),
		slog.Int("total_items", len(m.items)))

	return m, nil
}

func (m Model) handleFilterAdvanced() (Model, tea.Cmd) {
	slog.Info("User opened advanced filter dialog",
		slog.String("current_review_status", m.filterReviewStatus),
		slog.String("current_type", m.filterType),
		slog.String("current_repo", m.filterRepo))
	m.showAdvancedFilter = true
	return m, nil
}

func (m Model) handleHelp() (Model, tea.Cmd) {
	slog.Info("User toggled help")
	m.help.ShowAll = !m.help.ShowAll
	return m, nil
}

func (m Model) applyAdvancedFilters() Model {
	slog.Debug("Applying advanced filters",
		slog.String("review_status", m.filterReviewStatus),
		slog.String("type", m.filterType),
		slog.String("repo", m.filterRepo))

	// Update the legacy filter state to match advanced filter
	m.showOnlyUnreviewed = (m.filterReviewStatus == "unreviewed")

	// Update visible items based on new filter state
	slog.Debug("About to update visible items for advanced filters")
	m = m.updateVisibleItemsWithPreserveSelection(false)
	slog.Debug("Updated visible items for advanced filters", slog.Int("visible_count", len(m.list.Items())))

	// Update status message with active filters
	statusParts := []string{}

	if m.filterReviewStatus != "all" {
		statusParts = append(statusParts, m.filterReviewStatus+" PRs")
	} else {
		statusParts = append(statusParts, "all PRs")
	}

	if m.filterType != "all" {
		statusParts = append(statusParts, m.filterType+" changes")
	}

	if m.filterRepo != "all" {
		statusParts = append(statusParts, m.filterRepo+" repo")
	}

	if len(statusParts) > 1 {
		m.status = fmt.Sprintf("Showing %s", strings.Join(statusParts, ", "))
	} else if len(statusParts) == 1 {
		m.status = fmt.Sprintf("Showing %s", statusParts[0])
	} else {
		m.status = "Showing all PRs"
	}

	slog.Info("Advanced filters applied",
		slog.String("review_status", m.filterReviewStatus),
		slog.String("type", m.filterType),
		slog.String("repo", m.filterRepo),
		slog.Int("visible_items", len(m.list.Items())),
		slog.Int("total_items", len(m.items)))

	return m
}

func (m Model) updateVisibleItems() Model {
	return m.updateVisibleItemsWithPreserveSelection(true)
}

func (m Model) updateVisibleItemsWithPreserveSelection(preserveSelection bool) Model {
	if len(m.items) == 0 {
		slog.Debug("No items to filter", slog.Bool("preserve_selection", preserveSelection))
		return m
	}

	start := time.Now()

	slog.Debug("Starting filter operation",
		slog.String("review_status_filter", m.filterReviewStatus),
		slog.String("type_filter", m.filterType),
		slog.String("repo_filter", m.filterRepo),
		slog.Bool("preserve_selection", preserveSelection))

	// Get currently selected PR to prevent jarring disappearance (only during async updates)
	currentSelection := m.list.SelectedItem()
	var selectedPRNumber int
	if preserveSelection && currentSelection != nil {
		if prItem, ok := currentSelection.(PRItem); ok {
			selectedPRNumber = prItem.PR.Number
		}
	}

	var visibleItems []list.Item
	filteredCount := 0
	reviewedCount := 0
	approvedCount := 0
	dismissedCount := 0
	loadingCount := 0
	typeFilteredCount := 0
	repoFilteredCount := 0

	for _, item := range m.items {
		shouldShow := true

		// Count review states for logging
		if item.Reviewed {
			reviewedCount++
		}
		if item.Approved {
			approvedCount++
		}
		if item.Dismissed {
			dismissedCount++
		}
		if item.LoadingReviews {
			loadingCount++
		}

		// Apply review status filter
		switch m.filterReviewStatus {
		case "unreviewed":
			// Show PR if:
			// - Not reviewed AND not approved yet, OR
			// - Review was dismissed (needs re-review), OR
			// - Review status is still being loaded, OR
			// - It's the currently selected PR (prevent jarring disappearance)
			shouldShow = shouldShow && ((!item.Reviewed && !item.Approved) || item.Dismissed || item.LoadingReviews ||
				(selectedPRNumber > 0 && item.PR.Number == selectedPRNumber))
		case "reviewed":
			// Show only reviewed PRs (approved or other review states)
			shouldShow = shouldShow && (item.Reviewed || item.Approved)
		}
		// "all" - no review status filtering

		// Apply PR type filter
		if shouldShow && m.filterType != "all" {
			prType := m.determinePRType(item)
			matchesType := (prType == m.filterType)
			if !matchesType {
				typeFilteredCount++
				slog.Debug("PR filtered out by type",
					slog.Int("pr_number", item.PR.Number),
					slog.String("pr_title", item.PR.Title),
					slog.String("detected_type", prType),
					slog.String("filter_type", m.filterType))
			}
			shouldShow = shouldShow && matchesType
		}

		// Apply repository filter
		if shouldShow && m.filterRepo != "all" {
			if m.filterRepo == "current" {
				// For now, show all repos since we don't have a "current" repo concept
				// TODO: Implement current repo detection
				repoFilteredCount++ // Count as filtered for now
				slog.Debug("PR would be filtered by repo (not implemented)",
					slog.Int("pr_number", item.PR.Number),
					slog.String("repo", item.PR.Owner+"/"+item.PR.Repo))
				shouldShow = true // Keep all for now
			}
		}

		if shouldShow {
			visibleItems = append(visibleItems, item)
		} else {
			filteredCount++
			slog.Debug("PR filtered out",
				slog.Int("pr_number", item.PR.Number),
				slog.String("pr_title", item.PR.Title),
				slog.Bool("reviewed", item.Reviewed),
				slog.Bool("approved", item.Approved),
				slog.Bool("dismissed", item.Dismissed))
		}
	}

	duration := time.Since(start)

	slog.Debug("Updated visible items",
		slog.Bool("preserve_selection", preserveSelection),
		slog.String("filter_review_status", m.filterReviewStatus),
		slog.String("filter_type", m.filterType),
		slog.String("filter_repo", m.filterRepo),
		slog.Int("selected_pr", selectedPRNumber),
		slog.Int("total_items", len(m.items)),
		slog.Int("visible_items", len(visibleItems)),
		slog.Int("filtered_out", filteredCount),
		slog.Int("type_filtered_count", typeFilteredCount),
		slog.Int("repo_filtered_count", repoFilteredCount),
		slog.Int("reviewed_count", reviewedCount),
		slog.Int("approved_count", approvedCount),
		slog.Int("dismissed_count", dismissedCount),
		slog.Int("loading_count", loadingCount),
		slog.Duration("duration", duration))

	// Update the list with filtered items
	m.list.SetItems(visibleItems)

	return m
}

// determinePRType analyzes a PR to determine its type based on file changes
func (m Model) determinePRType(item PRItem) string {
	// If diff stats aren't loaded yet, return "mixed" as default
	if item.DiffStats == nil {
		slog.Debug("PR type detection: no diff stats available", slog.Any("pr", item.PR.Number))
		return "mixed"
	}

	// Analyze PR title and content for type hints
	title := strings.ToLower(item.PR.Title)

	// Check for dependency updates in title
	if slices.ContainsFunc(dependencyKeywords, func(keyword string) bool {
		return strings.Contains(title, keyword)
	}) {
		slog.Debug("PR type detection: detected dependencies from title", slog.String("title", item.PR.Title))
		return "dependencies"
	}

	// Check for documentation keywords in title
	if slices.ContainsFunc(documentationKeywords, func(keyword string) bool {
		return strings.Contains(title, keyword)
	}) {
		slog.Debug("PR type detection: detected docs from title", slog.String("title", item.PR.Title))
		return "docs"
	}

	// Use file count and change size as heuristics
	// Small changes with few files often indicate docs or config
	if item.DiffStats.Files <= 2 && item.DiffStats.Additions+item.DiffStats.Deletions < 100 {
		slog.Debug("PR type detection: small change, assuming docs",
			slog.Int("files", item.DiffStats.Files),
			slog.Int("total_changes", item.DiffStats.Additions+item.DiffStats.Deletions))
		return "docs"
	}

	// Large changes with many files suggest mixed or significant code changes
	if item.DiffStats.Files > 20 || item.DiffStats.Additions+item.DiffStats.Deletions > 1000 {
		slog.Debug("PR type detection: large change, assuming mixed",
			slog.Int("files", item.DiffStats.Files),
			slog.Int("total_changes", item.DiffStats.Additions+item.DiffStats.Deletions))
		return "mixed"
	}

	// Medium-sized changes default to code
	slog.Debug("PR type detection: medium change, assuming code",
		slog.Int("files", item.DiffStats.Files),
		slog.Int("total_changes", item.DiffStats.Additions+item.DiffStats.Deletions))
	return "code"
}

func (m Model) moveToNext() tea.Cmd {
	return func() tea.Msg {
		// Move to next item if not at the end
		if m.list.Index() < len(m.list.Items())-1 {
			return tea.KeyMsg{Type: tea.KeyDown}
		}
		return nil
	}
}

func (m Model) triggerAIAnalysisIfReady(itemIndex int) tea.Cmd {
	if m.aiAgent == nil {
		slog.Debug("AI agent is nil", slog.Int("itemIndex", itemIndex))
		return nil
	}

	item := &m.items[itemIndex]

	slog.Debug("Checking AI analysis conditions", slog.Any("pr", item.PR),
		slog.Bool("LoadingDiff", item.LoadingDiff),
		slog.Bool("LoadingChecks", item.LoadingChecks),
		slog.Bool("LoadingReviews", item.LoadingReviews),
		slog.Bool("LoadingAI", item.LoadingAI),
		slog.Bool("HasDiffStats", item.DiffStats != nil),
		slog.Bool("HasCheckStatus", item.CheckStatus != nil),
		slog.Bool("HasReviews", item.Reviews != nil),
		slog.Bool("HasDiffError", item.DiffError != nil),
		slog.Bool("HasCheckError", item.CheckError != nil),
		slog.Bool("HasReviewError", item.ReviewError != nil),
		slog.String("HeadSHA", item.PR.HeadSHA))

	// Check if we have all required data and haven't started AI analysis yet
	if !item.LoadingDiff && !item.LoadingChecks && !item.LoadingReviews &&
		item.LoadingAI && item.DiffStats != nil && item.CheckStatus != nil &&
		item.Reviews != nil && item.DiffError == nil && item.CheckError == nil && item.ReviewError == nil &&
		item.PR.HeadSHA != "" {

		slog.Debug("All conditions met, triggering AI analysis", slog.Any("pr", item.PR))
		return FetchAIAnalysisCmd(m.aiAgent, item.PR, item.DiffStats, item.CheckStatus, item.Reviews, item.ID, m.config.AI.AnalysisTimeout)
	}

	slog.Debug("AI analysis conditions not met", slog.Any("pr", item.PR))
	return nil
}

// generateDetailContent creates detailed content for a PR popup
func (m Model) generateDetailContent(item PRItem) string {
	var content strings.Builder

	// Header
	content.WriteString(fmt.Sprintf("# %s\n\n", item.PR.Title))
	content.WriteString(fmt.Sprintf("**Repository:** %s/%s\n", item.PR.Owner, item.PR.Repo))
	content.WriteString(fmt.Sprintf("**PR Number:** #%d\n", item.PR.Number))

	if !item.PR.UpdatedAt.IsZero() {
		content.WriteString(fmt.Sprintf("**Updated:** %s\n", item.PR.UpdatedAt.Format("Jan 2, 2006 at 3:04 PM")))
	}

	if item.PR.HeadSHA != "" {
		sha := item.PR.HeadSHA
		if len(sha) > 8 {
			sha = sha[:8]
		}
		content.WriteString(fmt.Sprintf("**Head SHA:** `%s`\n", sha))
	}

	content.WriteString("\n---\n\n")

	// Diff Stats
	if item.DiffStats != nil {
		content.WriteString("## 📊 Changes\n\n")
		content.WriteString(fmt.Sprintf("- **%d** additions\n", item.DiffStats.Additions))
		content.WriteString(fmt.Sprintf("- **%d** deletions\n", item.DiffStats.Deletions))
		content.WriteString(fmt.Sprintf("- **%d** files changed\n", item.DiffStats.Files))
		content.WriteString("\n")
	} else if item.LoadingDiff {
		content.WriteString("## 📊 Changes\n\n*Loading diff statistics...*\n\n")
	}

	// Check Status
	if item.CheckStatus != nil {
		content.WriteString("## ✅ Checks\n\n")
		content.WriteString(fmt.Sprintf("**Status:** %s\n", strings.ToUpper(string(item.CheckStatus.State[0]))+item.CheckStatus.State[1:]))
		if item.CheckStatus.Description != "" {
			content.WriteString(fmt.Sprintf("**Description:** %s\n", item.CheckStatus.Description))
		}

		if len(item.CheckStatus.Details) > 0 {
			content.WriteString("\n**Details:**\n")
			for _, detail := range item.CheckStatus.Details {
				status := "❓"
				switch detail.Status {
				case "success":
					status = "✅"
				case "failure", "error":
					status = "❌"
				case "pending", "in_progress":
					status = "⏳"
				}
				content.WriteString(fmt.Sprintf("- %s %s\n", status, detail.Name))
			}
		}
		content.WriteString("\n")
	} else if item.LoadingChecks {
		content.WriteString("## ✅ Checks\n\n*Loading check status...*\n\n")
	}

	// Reviews
	if item.Reviews != nil {
		content.WriteString("## 👥 Reviews\n\n")
		if len(item.Reviews) == 0 {
			content.WriteString("*No reviews yet*\n\n")
		} else {
			userReviewed := false
			userApproved := false

			for _, review := range item.Reviews {
				status := "💬"
				switch review.State {
				case "APPROVED":
					status = "✅"
				case "CHANGES_REQUESTED":
					status = "❌"
				case "COMMENTED":
					status = "💬"
				}

				content.WriteString(fmt.Sprintf("- %s %s: %s\n", status, review.User, review.State))

				if review.User == m.username {
					userReviewed = true
					if review.State == "APPROVED" {
						userApproved = true
					}
				}
			}

			content.WriteString("\n**Your Status:** ")
			if userApproved {
				content.WriteString("✅ Approved")
			} else if userReviewed {
				content.WriteString("👀 Reviewed")
			} else {
				content.WriteString("⏸️ Not reviewed")
			}
			content.WriteString("\n\n")
		}
	} else if item.LoadingReviews {
		content.WriteString("## 👥 Reviews\n\n*Loading reviews...*\n\n")
	}

	// AI Analysis
	if item.AIAnalysis != nil {
		content.WriteString("## 🤖 AI Analysis\n\n")
		content.WriteString(fmt.Sprintf("**Risk Level:** %s\n", item.AIAnalysis.RiskLevel))
		content.WriteString(fmt.Sprintf("**Recommendation:** %s\n", item.AIAnalysis.Recommendation))
		if item.AIAnalysis.Reasoning != "" {
			content.WriteString(fmt.Sprintf("\n**Reasoning:**\n%s\n", item.AIAnalysis.Reasoning))
		}
		content.WriteString("\n")
	} else if item.LoadingAI {
		content.WriteString("## 🤖 AI Analysis\n\n*Running AI analysis...*\n\n")
	} else if m.aiAgent != nil {
		content.WriteString("## 🤖 AI Analysis\n\n*AI analysis will run when all data is loaded*\n\n")
	}

	// Footer
	content.WriteString("---\n\n")
	content.WriteString("*Press **Enter** or **Esc** to close*")

	return content.String()
}

// renderAdvancedFilterDialog renders the advanced filter dialog
func (m Model) renderAdvancedFilterDialog(baseView string) string {
	// Get terminal dimensions from the list widget
	width := m.list.Width()
	height := m.list.Height() + 4 // Account for status and help

	// Define dialog dimensions
	dialogWidth := min(width*8/10, 80)

	// Create dialog content
	var content strings.Builder
	content.WriteString("Advanced Filter Options\n\n")

	// Review Status Section
	content.WriteString("Review Status:\n")
	reviewOptions := []struct {
		key   string
		value string
		label string
	}{
		{"1", "all", "All PRs"},
		{"2", "unreviewed", "Unreviewed PRs"},
		{"3", "reviewed", "Reviewed PRs"},
	}

	for _, option := range reviewOptions {
		indicator := "○ "
		if m.filterReviewStatus == option.value {
			indicator = "● "
		}
		content.WriteString(fmt.Sprintf("  %s%s %s\n", indicator, option.key, option.label))
	}

	content.WriteString("\n")

	// PR Type Section
	content.WriteString("PR Type:\n")
	typeOptions := []struct {
		key   string
		value string
		label string
	}{
		{"4", "all", "All types"},
		{"5", "docs", "Documentation"},
		{"6", "code", "Code changes"},
		{"7", "dependencies", "Dependencies"},
		{"8", "mixed", "Mixed changes"},
	}

	for _, option := range typeOptions {
		indicator := "○ "
		if m.filterType == option.value {
			indicator = "● "
		}
		content.WriteString(fmt.Sprintf("  %s%s %s\n", indicator, option.key, option.label))
	}

	content.WriteString("\n")

	// Repository Filter Section
	content.WriteString("Repository Filter:\n")
	repoOptions := []struct {
		key   string
		value string
		label string
	}{
		{"9", "all", "All repositories"},
		{"0", "current", "Current repository only"},
	}

	for _, option := range repoOptions {
		indicator := "○ "
		if m.filterRepo == option.value {
			indicator = "● "
		}
		content.WriteString(fmt.Sprintf("  %s%s %s\n", indicator, option.key, option.label))
	}

	content.WriteString("\nPress Enter to apply filters or Esc to cancel")

	// Create dialog border style
	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("75")). // Blue border for filter dialog
		Background(lipgloss.Color("235")).
		Foreground(lipgloss.Color("255")).
		Padding(1).
		Width(dialogWidth - 4) // Account for border and padding

	dialog := borderStyle.Render(content.String())

	// Center the dialog on screen
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, dialog)
}

// renderPopup renders the popup overlay
func (m Model) renderPopup(baseView string) string {
	// Get terminal dimensions from the list widget
	width := m.list.Width()
	height := m.list.Height() + 4 // Account for status and help

	// Define popup dimensions (80% of screen to leave more background visible)
	popupWidth := min(width*8/10, 100)
	popupHeight := min(height*8/10, 35)

	// Format content and handle scrolling
	formattedContent := m.formatPopupContent(m.popupContent, popupWidth-6)
	contentLines := strings.Split(formattedContent, "\n")

	// Calculate visible area (reserve space for border and padding)
	visibleHeight := popupHeight - 4 // Account for border (2) + padding (2)

	// Ensure scroll position is within bounds
	maxScroll := max(0, len(contentLines)-visibleHeight)
	scrollPos := min(m.popupScrollPos, maxScroll)

	// Extract visible content
	var visibleLines []string
	if len(contentLines) > visibleHeight {
		end := min(scrollPos+visibleHeight, len(contentLines))
		visibleLines = contentLines[scrollPos:end]

		// Add scroll indicators
		if scrollPos > 0 {
			// Replace first line with scroll up indicator
			if len(visibleLines) > 0 {
				visibleLines[0] = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("↑ (more above)")
			}
		}
		if scrollPos+visibleHeight < len(contentLines) {
			// Replace last line with scroll down indicator
			if len(visibleLines) > 0 {
				visibleLines[len(visibleLines)-1] = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("↓ (more below)")
			}
		}
	} else {
		visibleLines = contentLines
	}

	content := strings.Join(visibleLines, "\n")

	// Create popup border style with semi-transparent background
	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("205")).
		Background(lipgloss.Color("235")). // Slightly lighter background for contrast
		Foreground(lipgloss.Color("255")). // Bright white text
		Padding(1).
		Width(popupWidth - 4) // Account for border and padding

	popup := borderStyle.Render(content)

	// Simple, clean popup overlay using lipgloss.Place()
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, popup)
}

// formatPopupContent applies basic markdown-like formatting
func (m Model) formatPopupContent(content string, maxWidth int) string {
	lines := strings.Split(content, "\n")
	var formatted strings.Builder

	for _, line := range lines {
		// Handle headers
		if after, ok := strings.CutPrefix(line, "# "); ok {
			text := after
			formatted.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205")).Render(text))
		} else if after, ok := strings.CutPrefix(line, "## "); ok {
			text := after
			formatted.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("75")).Render(text))
		} else if strings.HasPrefix(line, "**") && strings.HasSuffix(line, "**") {
			text := strings.TrimSuffix(strings.TrimPrefix(line, "**"), "**")
			formatted.WriteString(lipgloss.NewStyle().Bold(true).Render(text))
		} else if strings.HasPrefix(line, "*") && strings.HasSuffix(line, "*") && !strings.HasPrefix(line, "**") {
			text := strings.TrimSuffix(strings.TrimPrefix(line, "*"), "*")
			formatted.WriteString(lipgloss.NewStyle().Italic(true).Render(text))
		} else if strings.HasPrefix(line, "`") && strings.HasSuffix(line, "`") {
			text := strings.TrimSuffix(strings.TrimPrefix(line, "`"), "`")
			formatted.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("86")).Render(text))
		} else {
			// Word wrap long lines
			if len(line) > maxWidth {
				words := strings.Fields(line)
				currentLine := ""
				for _, word := range words {
					if len(currentLine)+len(word)+1 > maxWidth {
						if currentLine != "" {
							formatted.WriteString(currentLine + "\n")
							currentLine = word
						} else {
							formatted.WriteString(word + "\n")
						}
					} else {
						if currentLine == "" {
							currentLine = word
						} else {
							currentLine += " " + word
						}
					}
				}
				if currentLine != "" {
					formatted.WriteString(currentLine)
				}
			} else {
				formatted.WriteString(line)
			}
		}
		formatted.WriteString("\n")
	}

	return formatted.String()
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// max returns the maximum of two integers
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
