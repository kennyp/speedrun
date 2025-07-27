package ui

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

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
}

// KeyMap defines key bindings
type KeyMap struct {
	Approve   key.Binding
	Skip      key.Binding
	View      key.Binding
	Filter    key.Binding
	Details   key.Binding
	Quit      key.Binding
	Refresh   key.Binding
}

// DefaultKeyMap returns the default key bindings
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Approve: key.NewBinding(
			key.WithKeys("a"),
			key.WithHelp("a", "approve"),
		),
		Skip: key.NewBinding(
			key.WithKeys("s"),
			key.WithHelp("s", "skip"),
		),
		View: key.NewBinding(
			key.WithKeys("v"),
			key.WithHelp("v", "view in browser"),
		),
		Filter: key.NewBinding(
			key.WithKeys("f"),
			key.WithHelp("f", "toggle filter"),
		),
		Details: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "show details"),
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

// NewModel creates a new TUI model
func NewModel(ctx context.Context, cfg *config.Config, githubClient *github.Client, aiAgent *agent.Agent, username string) Model {
	// Create list
	l := list.New([]list.Item{}, list.NewDefaultDelegate(), 0, 0)
	l.Title = fmt.Sprintf("🔍 Pull Requests for %s", username)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	l.Styles.Title = titleStyle

	// Create spinner
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	return Model{
		ctx:      ctx,
		config:   cfg,
		github:   githubClient,
		aiAgent:  aiAgent,
		username: username,
		list:     l,
		items:    []PRItem{},
		status:   "Loading pull requests...",
		spinner:  s,
		keys:     DefaultKeyMap(),
		loadingPRs: true,
		showOnlyUnreviewed: true, // Default to showing only unreviewed PRs
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
		// Handle popup-specific keys first
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

		case key.Matches(msg, m.keys.Skip):
			return m.handleSkip()

		case key.Matches(msg, m.keys.View):
			return m.handleView()

		case key.Matches(msg, m.keys.Filter):
			return m.handleFilter()

		case key.Matches(msg, m.keys.Details):
			return m.handleDetails()

		case key.Matches(msg, m.keys.Refresh):
			return m.handleRefresh()
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

	case SmartRefreshLoadedMsg:
		return m.handleSmartRefreshLoaded(msg)

	case PRApprovedMsg:
		return m.handlePRApproved(msg)

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
	var help string
	if m.showPopup {
		help = helpStyle.Render("↑/k: scroll up • ↓/j: scroll down • pgup/pgdown: page • home/end: top/bottom • enter/esc: close")
	} else {
		help = helpStyle.Render("a: approve • s: skip • v: view • enter: details • f: toggle filter • r: refresh • q: quit")
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
		help,
	)
	
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
		loadingAI := m.aiAgent != nil
		if m.aiAgent != nil {
			if cachedAnalysis, err := pr.GetCachedAIAnalysis(); err == nil {
				if _, ok := cachedAnalysis.(*agent.Analysis); ok {
					loadingAI = false // Don't show loading if we have cached analysis
				}
			}
		}
		
		m.items[i] = PRItem{
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

	// Start loading details for all PRs
	cmds := []tea.Cmd{}
	for i, pr := range msg.PRs {
		// Stagger requests to avoid rate limiting
		delay := time.Duration(i*50) * time.Millisecond
		cmds = append(cmds,
			tea.Tick(delay, func(t time.Time) tea.Msg {
				return FetchDiffStatsCmd(m.github, pr)()
			}),
			tea.Tick(delay+20*time.Millisecond, func(t time.Time) tea.Msg {
				return FetchCheckStatusCmd(m.github, pr)()
			}),
			tea.Tick(delay+40*time.Millisecond, func(t time.Time) tea.Msg {
				return FetchReviewsCmd(m.github, pr, m.username)()
			}),
		)
		
		// Load cached AI analysis immediately if available
		if !m.items[i].LoadingAI {
			cmds = append(cmds, FetchCachedAIAnalysisCmd(pr))
		}
	}

	return m, tea.Batch(cmds...)
}

func (m Model) handleDiffStatsLoaded(msg DiffStatsLoadedMsg) (Model, tea.Cmd) {
	// Find the PR item
	for i := range m.items {
		if m.items[i].PR.Number == msg.PRNumber {
			m.items[i].LoadingDiff = false
			m.items[i].DiffStats = msg.Stats
			m.items[i].DiffError = msg.Err
			
			// Re-apply filter to update the visible list
			m = m.updateVisibleItems()
			
			// Trigger AI analysis if we have all required data and AI agent is available
			cmd := m.triggerAIAnalysisIfReady(i)
			return m, cmd
		}
	}
	
	return m, nil
}

func (m Model) handleCheckStatusLoaded(msg CheckStatusLoadedMsg) (Model, tea.Cmd) {
	// Find the PR item
	for i := range m.items {
		if m.items[i].PR.Number == msg.PRNumber {
			m.items[i].LoadingChecks = false
			m.items[i].CheckStatus = msg.Status
			m.items[i].CheckError = msg.Err
			
			// Re-apply filter to update the visible list
			m = m.updateVisibleItems()
			
			// Trigger AI analysis if we have all required data and AI agent is available
			cmd := m.triggerAIAnalysisIfReady(i)
			return m, cmd
		}
	}
	
	return m, nil
}

func (m Model) handleReviewsLoaded(msg ReviewsLoadedMsg) (Model, tea.Cmd) {
	// Find the PR item
	for i := range m.items {
		if m.items[i].PR.Number == msg.PRNumber {
			m.items[i].LoadingReviews = false
			m.items[i].Reviews = msg.Reviews
			m.items[i].ReviewError = msg.Err
			
			// Check if current user has reviewed and determine review type
			userReviewed := false
			userApproved := false
			for _, review := range msg.Reviews {
				if review.User == m.username {
					userReviewed = true
					if review.State == "APPROVED" {
						userApproved = true
					}
					// Note: We don't break here because there might be multiple reviews
					// and we want to find the most recent APPROVED status
				}
			}
			
			m.items[i].Reviewed = userReviewed
			m.items[i].Approved = userApproved
			
			slog.Debug("Reviews loaded for PR", slog.Any("pr", m.items[i].PR), 
				slog.Int("total_reviews", len(msg.Reviews)), slog.Bool("user_reviewed", userReviewed), 
				slog.Bool("user_approved", userApproved), slog.Any("error", msg.Err))
			
			// Re-apply filter since review status may have changed
			m = m.updateVisibleItems()
			
			// Trigger AI analysis if we have all required data and AI agent is available
			cmd := m.triggerAIAnalysisIfReady(i)
			return m, cmd
		}
	}
	
	slog.Debug("Reviews loaded for unknown PR", slog.Int("pr_number", msg.PRNumber))
	return m, nil
}

func (m Model) handleAIAnalysisLoaded(msg AIAnalysisLoadedMsg) (Model, tea.Cmd) {
	// Find the PR item
	for i := range m.items {
		if m.items[i].PR.Number == msg.PRNumber {
			m.items[i].LoadingAI = false
			m.items[i].AIAnalysis = msg.Analysis
			m.items[i].AIError = msg.Err
			
			// Re-apply filter to update the visible list
			m = m.updateVisibleItems()
			break
		}
	}
	
	return m, nil
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
		delay := time.Duration(i*50) * time.Millisecond
		
		// Load diff stats if needed
		if item.LoadingDiff {
			cmds = append(cmds, tea.Tick(delay, func(t time.Time) tea.Msg {
				return FetchDiffStatsCmd(m.github, pr)()
			}))
		}
		
		// Load check status if needed  
		if item.LoadingChecks {
			cmds = append(cmds, tea.Tick(delay+20*time.Millisecond, func(t time.Time) tea.Msg {
				return FetchCheckStatusCmd(m.github, pr)()
			}))
		}
		
		// Always refresh reviews (user might have reviewed)
		if item.LoadingReviews {
			cmds = append(cmds, tea.Tick(delay+40*time.Millisecond, func(t time.Time) tea.Msg {
				return FetchReviewsCmd(m.github, pr, m.username)()
			}))
		}
	}
	
	return m, tea.Batch(cmds...)
}

func (m Model) handlePRApproved(msg PRApprovedMsg) (Model, tea.Cmd) {
	if msg.Err != nil {
		slog.Error("PR approval failed in UI", slog.Int("pr_number", msg.PRNumber), slog.Any("error", msg.Err))
		m.status = errorStyle.Render("Failed to approve PR: " + msg.Err.Error())
		return m, nil
	}

	// Find and update the PR item
	for i := range m.items {
		if m.items[i].PR.Number == msg.PRNumber {
			m.items[i].Approved = true
			m.items[i].Reviewed = true
			
			slog.Info("PR approved successfully in UI", slog.Any("pr", m.items[i].PR))
			m.status = successStyle.Render(fmt.Sprintf("✅ Approved PR #%d", msg.PRNumber))
			break
		}
	}

	// Re-apply filter since review status changed
	m = m.updateVisibleItems()

	// Move to next item
	return m, m.moveToNext()
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
	return m, ApprovePRCmd(prItem.PR)
}

func (m Model) handleSkip() (Model, tea.Cmd) {
	selected := m.list.SelectedItem()
	if selected == nil {
		slog.Debug("Skip action: no PR selected")
		return m, nil
	}

	prItem, ok := selected.(PRItem)
	if !ok {
		slog.Debug("Skip action: selected item is not a PR")
		return m, nil
	}

	slog.Info("User skipped PR", slog.Any("pr", prItem.PR), 
		slog.Bool("reviewed", prItem.Reviewed), slog.Bool("approved", prItem.Approved))
	m.status = fmt.Sprintf("⏭️ Skipped PR #%d", prItem.PR.Number)
	return m, m.moveToNext()
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
	// Toggle filter state
	oldFilter := m.showOnlyUnreviewed
	m.showOnlyUnreviewed = !m.showOnlyUnreviewed
	
	slog.Debug("Filter toggled", slog.Bool("old_filter", oldFilter), slog.Bool("new_filter", m.showOnlyUnreviewed), 
		slog.Int("total_items", len(m.items)))
	
	// Update visible items based on new filter state (don't preserve selection for user-initiated filter)
	m = m.updateVisibleItemsWithPreserveSelection(false)
	
	// Update status message
	filterStatus := "all"
	if m.showOnlyUnreviewed {
		filterStatus = "unreviewed only"
	}
	m.status = fmt.Sprintf("Filter toggled: showing %s PRs", filterStatus)
	
	slog.Info("Filter applied", slog.String("filter_mode", filterStatus), 
		slog.Int("visible_items", len(m.list.Items())), slog.Int("total_items", len(m.items)))
	
	return m, nil
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
	loadingCount := 0
	
	for _, item := range m.items {
		shouldShow := false
		
		// Count review states for logging
		if item.Reviewed {
			reviewedCount++
		}
		if item.Approved {
			approvedCount++
		}
		if item.LoadingReviews {
			loadingCount++
		}
		
		if m.showOnlyUnreviewed {
			// Show PR if:
			// - Not reviewed AND not approved yet, OR
			// - Review status is still being loaded, OR  
			// - It's the currently selected PR (prevent jarring disappearance)
			shouldShow = (!item.Reviewed && !item.Approved) || item.LoadingReviews || 
						 (selectedPRNumber > 0 && item.PR.Number == selectedPRNumber)
		} else {
			// Show all PRs
			shouldShow = true
		}
		
		if shouldShow {
			visibleItems = append(visibleItems, item)
		} else {
			filteredCount++
		}
	}
	
	duration := time.Since(start)
	
	slog.Debug("Updated visible items", 
		slog.Bool("preserve_selection", preserveSelection),
		slog.Bool("show_only_unreviewed", m.showOnlyUnreviewed),
		slog.Int("selected_pr", selectedPRNumber),
		slog.Int("total_items", len(m.items)),
		slog.Int("visible_items", len(visibleItems)),
		slog.Int("filtered_out", filteredCount),
		slog.Int("reviewed_count", reviewedCount),
		slog.Int("approved_count", approvedCount),
		slog.Int("loading_count", loadingCount),
		slog.Duration("duration", duration))
	
	// Update the list with filtered items
	m.list.SetItems(visibleItems)
	
	return m
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
		return nil
	}
	
	item := &m.items[itemIndex]
	
	// Check if we have all required data and haven't started AI analysis yet
	if !item.LoadingDiff && !item.LoadingChecks && !item.LoadingReviews && 
	   item.LoadingAI && item.DiffStats != nil && item.CheckStatus != nil && 
	   item.Reviews != nil && item.DiffError == nil && item.CheckError == nil && item.ReviewError == nil {
		
		return FetchAIAnalysisCmd(m.aiAgent, item.PR, item.DiffStats, item.CheckStatus, item.Reviews)
	}
	
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
		content.WriteString(fmt.Sprintf("**Status:** %s\n", strings.Title(item.CheckStatus.State)))
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
			
			content.WriteString(fmt.Sprintf("\n**Your Status:** "))
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
		if strings.HasPrefix(line, "# ") {
			text := strings.TrimPrefix(line, "# ")
			formatted.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205")).Render(text))
		} else if strings.HasPrefix(line, "## ") {
			text := strings.TrimPrefix(line, "## ")
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