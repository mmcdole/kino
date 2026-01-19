package components

import (
	"fmt"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/drake/goplex/internal/domain"
	"github.com/drake/goplex/internal/tui/styles"
)

// LibraryStatus represents the sync status of a library
type LibraryStatus int

const (
	StatusIdle LibraryStatus = iota
	StatusSyncing
	StatusSynced
	StatusError
)

// LibrarySyncState tracks sync progress for a single library
type LibrarySyncState struct {
	Status   LibraryStatus
	Loaded   int   // Items loaded so far
	Total    int   // Total items expected
	FromDisk bool  // Whether loaded from cache
	Error    error // Error if any
}

// LibraryItem implements list.Item for libraries
type LibraryItem struct {
	Library domain.Library
	State   LibrarySyncState
	Frame   int
}

// Spinner frames for syncing animation
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func (i LibraryItem) FilterValue() string { return i.Library.Name }

func (i LibraryItem) Title() string {
	switch i.State.Status {
	case StatusSyncing:
		spinner := spinnerFrames[i.Frame%len(spinnerFrames)]
		if i.State.Total > 0 {
			// Show progress: "⠋ Movies (150/1200)"
			return fmt.Sprintf("%s %s (%d/%d)", spinner, i.Library.Name, i.State.Loaded, i.State.Total)
		}
		return spinner + " " + i.Library.Name
	case StatusSynced:
		// Show count: "✓ Movies (1200)"
		return fmt.Sprintf("✓ %s (%d)", i.Library.Name, i.State.Loaded)
	case StatusError:
		return "✗ " + i.Library.Name
	default:
		return "  " + i.Library.Name
	}
}

func (i LibraryItem) Description() string { return i.Library.Type }

// Border overhead for the sidebar panel
const BorderSize = 2

// Sidebar is the library selection sidebar component
type Sidebar struct {
	list          list.Model
	focused       bool
	width         int
	height        int
	selected      int
	libraries     []domain.Library
	libraryStates map[string]LibrarySyncState
	spinnerFrame  int
}

// NewSidebar creates a new sidebar component
func NewSidebar() Sidebar {
	delegate := list.NewDefaultDelegate()
	delegate.ShowDescription = false
	delegate.SetSpacing(0)

	delegate.Styles.SelectedTitle = lipgloss.NewStyle().
		Foreground(styles.White).
		Background(styles.SlateLight).
		Padding(0, 1)
	delegate.Styles.NormalTitle = lipgloss.NewStyle().
		Foreground(styles.LightGray).
		Padding(0, 1)

	l := list.New([]list.Item{}, delegate, 0, 0)
	l.Title = "Libraries"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.SetShowHelp(false)
	l.SetShowPagination(false)
	l.Styles.Title = lipgloss.NewStyle().
		Foreground(styles.PlexOrange).
		Bold(true).
		Padding(0, 1)

	return Sidebar{
		list:          l,
		libraryStates: make(map[string]LibrarySyncState),
	}
}

// SetLibraries updates the libraries in the sidebar
func (s *Sidebar) SetLibraries(libraries []domain.Library) {
	s.libraries = libraries
	s.refreshItems()
}

// SetLibraryStates updates the library sync states
func (s *Sidebar) SetLibraryStates(states map[string]LibrarySyncState) {
	s.libraryStates = states
	s.refreshItems()
}

// SetSpinnerFrame updates the spinner animation frame
func (s *Sidebar) SetSpinnerFrame(frame int) {
	s.spinnerFrame = frame
}

// RefreshItems rebuilds the list items with current state
func (s *Sidebar) RefreshItems() {
	s.refreshItems()
}

// refreshItems rebuilds the list items with current state
func (s *Sidebar) refreshItems() {
	items := make([]list.Item, len(s.libraries))
	for i, lib := range s.libraries {
		state := s.libraryStates[lib.ID]
		items[i] = LibraryItem{
			Library: lib,
			State:   state,
			Frame:   s.spinnerFrame,
		}
	}
	s.list.SetItems(items)
}

// SetSize updates the component dimensions
func (s *Sidebar) SetSize(width, height int) {
	s.width = width
	s.height = height
	s.list.SetSize(width-BorderSize, height-BorderSize)
}

// SetFocused sets the focus state
func (s *Sidebar) SetFocused(focused bool) {
	s.focused = focused
}

// IsFocused returns the focus state
func (s Sidebar) IsFocused() bool {
	return s.focused
}

// SelectedLibrary returns the currently selected library
func (s Sidebar) SelectedLibrary() *domain.Library {
	item := s.list.SelectedItem()
	if item == nil {
		return nil
	}
	libItem := item.(LibraryItem)
	return &libItem.Library
}

// SelectedIndex returns the selected index
func (s Sidebar) SelectedIndex() int {
	return s.list.Index()
}

// SetSelectedIndex sets the selected index
func (s *Sidebar) SetSelectedIndex(index int) {
	s.list.Select(index)
}

// Init initializes the component
func (s Sidebar) Init() tea.Cmd {
	return nil
}

// Update handles messages
func (s Sidebar) Update(msg tea.Msg) (Sidebar, tea.Cmd) {
	if !s.focused {
		return s, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "j", "down":
			s.list.CursorDown()
		case "k", "up":
			s.list.CursorUp()
		case "g":
			s.list.Select(0)
		case "G":
			s.list.Select(len(s.list.Items()) - 1)
		}
	}

	return s, nil
}

// View renders the component
func (s Sidebar) View() string {
	style := styles.InactiveBorder
	if s.focused {
		style = styles.ActiveBorder
	}

	// Subtract frame (border) size so total rendered size equals s.width x s.height
	frameW, frameH := style.GetFrameSize()

	return style.
		Width(s.width - frameW).
		Height(s.height - frameH).
		Render(s.list.View())
}
