package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mmcdole/kino/internal/catalog"
	"github.com/mmcdole/kino/internal/domain"
	"github.com/mmcdole/kino/internal/search"
	"github.com/mmcdole/kino/internal/tui/components"
)

// Catalog is the collection data the TUI browses and changes. The TUI has no
// cache or backend access: all data reaches the model through a scoped result.
type Catalog interface {
	Load(context.Context, catalog.Resource, catalog.Policy) (catalog.Snapshot, error)
	Updates(context.Context) ([]catalog.State, error)
	Mutate(context.Context, catalog.Mutation) (catalog.Change, error)
	PlaylistMembership(context.Context, string) (catalog.Membership, error)
}

// Playback opens a media item in an external player.
type Playback interface {
	Play(context.Context, domain.MediaItem) error
	Resume(context.Context, domain.MediaItem) error
}

// Session ends the signed-in session. The cache is cleared by the caller
// after catalog work has drained.
type Session interface {
	Logout() error
}

// Options are the user's display preferences.
type Options struct {
	ShowWatchStatus   bool
	ShowLibraryCounts bool
}

// Model is the Bubble Tea model for the whole application.
type Model struct {
	overlay       overlay
	ready         bool
	width, height int
	spinnerFrame  int
	ticking       bool // a spinner tick is scheduled
	showInspector bool
	LoggedOut     bool
	loggingOut    bool

	catalog     Catalog
	playback    Playback
	searchIndex *search.Index
	session     Session
	opts        Options

	columns       *columnStack
	inspector     components.Inspector
	globalSearch  components.GlobalSearch
	sortModal     components.SortModal
	playlistModal components.PlaylistModal
	inputModal    components.InputModal

	libraries     []domain.Library
	libraryStates map[string]components.CollectionFeedback
	collections   map[string]catalog.State
	indicators    map[string]uint64 // key → server attempt whose spinner is due
	requests      *requests

	notice        notice
	noticeSeq     int
	searchSeq     uint64
	pendingSelect *pendingSelect
	confirmDelete *domain.Playlist // the playlist overlayConfirmDelete asks about
}

func NewModel(ctx context.Context, svc Catalog, playback Playback, session Session, index *search.Index, opts Options) *Model {
	m := &Model{
		catalog: svc, playback: playback, session: session, searchIndex: index, opts: opts,
		columns:       newColumnStack(),
		inspector:     components.NewInspector(),
		globalSearch:  components.NewGlobalSearch(),
		playlistModal: components.NewPlaylistModal(),
		inputModal:    components.NewInputModal(),
		libraryStates: make(map[string]components.CollectionFeedback),
		requests:      newRequests(ctx),
		collections:   make(map[string]catalog.State),
		indicators:    make(map[string]uint64),
	}
	root := catalog.Resource{Kind: catalog.Libraries}
	col := components.NewListColumn("Libraries", components.ColumnOptions{})
	col.SetContentID(root.Key())
	col.SetFeedback(components.CollectionFeedback{Pending: true})
	col.SetShowWatchStatus(opts.ShowWatchStatus)
	col.SetShowLibraryCounts(opts.ShowLibraryCounts)
	m.columns.Reset(col)
	m.track(root)
	return m
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		listen(m.catalog, m.requests.ctx),
		m.loadResource(catalog.Resource{Kind: catalog.Libraries}, catalog.Revalidate, false),
	)
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmd := m.update(msg)
	m.updateInspector()
	// The spinner only ticks while something on screen animates, so an idle
	// window does not redraw.
	if !m.ticking && m.animating() {
		m.ticking = true
		cmd = tea.Batch(cmd, tick())
	}
	return m, cmd
}

func (m *Model) update(msg tea.Msg) tea.Cmd {
	if m.loggingOut {
		if result, ok := msg.(logoutCompleteMsg); ok {
			if result.Error != nil {
				m.loggingOut = false
				m.overlay = overlayNone
				return m.notifyError("Logout failed", result.Error)
			}
			m.LoggedOut = true
			m.requests.cancel()
			return tea.Quit
		}
		return nil
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height, m.ready = msg.Width, msg.Height, true
		m.updateLayout()
		return nil
	case tea.KeyMsg:
		return m.handleKeyMsg(msg)
	case tickMsg:
		m.spinnerFrame++
		m.columns.UpdateSpinnerFrame(m.spinnerFrame)
		if !m.animating() {
			m.ticking = false
			return nil
		}
		return tick()
	case statesMsg:
		var cmds []tea.Cmd
		for _, st := range msg {
			cmds = append(cmds, m.applyState(st))
		}
		return tea.Batch(append(cmds, listen(m.catalog, m.requests.ctx))...)
	case loadDoneMsg:
		return m.handleLoadDone(msg)
	case actionMsg:
		return m.handleAction(msg)
	case playlistModalDataMsg:
		if !m.requests.owns(msg.Request) {
			return nil
		}
		m.requests.finish(msg.Request)
		if msg.Err != nil {
			m.overlay = overlayNone
			return m.notifyError("Loading playlists", msg.Err)
		}
		m.playlistModal.Show(msg.Membership.Playlists, msg.Membership.Present, &msg.Item)
		m.playlistModal.SetSize(m.width, m.height)
		return nil
	case clearNoticeMsg:
		m.expireNotice(msg.Seq)
		return nil
	case showLoadingMsg:
		if st := m.collections[msg.Key]; st.Fetching && st.Attempt == msg.Attempt {
			m.indicators[msg.Key] = msg.Attempt
			m.updateResourceFeedback(st.Resource)
		}
		return nil
	case searchDebounceMsg:
		return m.runSearch(msg)
	case showSearchLoadingMsg:
		if m.overlay == overlaySearch && msg.Seq == m.searchSeq {
			m.globalSearch.ShowLoading()
		}
		return nil
	case searchResultsMsg:
		m.showSearchResults(msg)
		return nil
	case searchIndexChangedMsg:
		if m.overlay == overlaySearch {
			return m.scheduleSearch()
		}
		return nil
	}
	// Bubble Tea text-input cursor messages belong to the open overlay too.
	var cmd tea.Cmd
	switch m.overlay {
	case overlaySearch:
		m.globalSearch, cmd, _ = m.globalSearch.Update(msg)
	case overlayInput:
		m.inputModal, cmd, _ = m.inputModal.Update(msg)
	}
	return cmd
}

// animating reports whether any spinner is on screen.
func (m *Model) animating() bool {
	for key := range m.indicators {
		if m.showsActivity(m.collections[key]) {
			return true
		}
	}
	return m.pendingActions() > 0
}

// pendingActions counts writes and playback launches awaiting a result.
func (m *Model) pendingActions() int {
	n := 0
	for owner := range m.requests.active {
		if strings.HasPrefix(owner, "mutation:") || strings.HasPrefix(owner, "playback:") {
			n++
		}
	}
	return n
}

func (m *Model) updateInspector() {
	if col := m.columns.Top(); col != nil {
		m.inspector.SetItem(col.SelectedItem())
	} else {
		m.inspector.SetItem(nil)
	}
}
