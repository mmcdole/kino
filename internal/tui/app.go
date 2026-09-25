package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mmcdole/kino/internal/catalog"
	"github.com/mmcdole/kino/internal/domain"
	"github.com/mmcdole/kino/internal/search"
	"github.com/mmcdole/kino/internal/tui/components"
)

// authFailedStatusMsg tells the user how to recover from a revoked/expired
// token. Shown persistently (not auto-cleared) since action is required.
const authFailedStatusMsg = "Session expired or revoked — press L to log out, then run kino to sign in again"

// Layout proportions for Miller Columns
const (
	// 3-Column Smart Ratios (Inspector visible)
	parentColumnPercent3   = 25 // Parent context
	inspectorColumnPercent = 30 // Inspector (summary)

	// 3-Column Focus Mode (Inspector hidden) - show more navigation context
	grandparentColumnPercent = 25 // Grandparent context
	parentColumnPercent2     = 30 // Parent context

	// Root level (single column + inspector)
	rootColumnPercent = 40

	minColumnWidth = 15

	// Vertical layout: single footer line
	chromeHeight = 1
)

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
		if m.overlay != overlaySearch || msg.Seq != m.searchSeq {
			return nil
		}
		req := m.requests.begin("search", catalog.Resource{}, catalog.Browse)
		libraries := append([]domain.Library(nil), m.libraries...)
		return func() tea.Msg {
			return searchResultsMsg{Request: req, Results: m.searchIndex.Search(req.ctx, msg.Query, libraries)}
		}
	case showSearchLoadingMsg:
		if m.overlay == overlaySearch && msg.Seq == m.searchSeq {
			m.globalSearch.ShowLoading()
		}
		return nil
	case searchResultsMsg:
		if !m.requests.owns(msg.Request) || m.overlay != overlaySearch {
			return nil
		}
		m.requests.finish(msg.Request)
		m.globalSearch.SetResults(msg.Results)
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

// handleLoadDone reports the outcome of this model's own request. Content
// and loading state come from the catalog's published state instead.
func (m *Model) handleLoadDone(msg loadDoneMsg) tea.Cmd {
	if !m.requests.owns(msg.Request) {
		return nil
	}
	m.requests.finish(msg.Request)
	r := msg.Request.Resource
	var cmds []tea.Cmd
	if msg.Err != nil {
		if p := m.pendingSelect; p != nil && p.key == r.Key() {
			m.pendingSelect = nil
		}
		cmds = append(cmds, m.notifyError("Loading "+m.resourceName(r), msg.Err))
	} else {
		// The load has settled, including from a fresh cache that publishes
		// no new state, so a pending selection is decided now.
		cmds = append(cmds, m.trySelect(r.Key(), true))
	}
	if msg.Warning != nil {
		cmds = append(cmds, m.notifyError("Loaded "+m.resourceName(r), msg.Warning))
	}
	m.updateResourceFeedback(r)
	return tea.Batch(cmds...)
}

func (m *Model) handleAction(msg actionMsg) tea.Cmd {
	if !m.requests.owns(msg.Request) {
		return nil
	}
	m.requests.finish(msg.Request)
	if msg.Playback {
		if msg.Err != nil {
			return m.notifyError("Starting playback", msg.Err)
		}
		return m.notify(noticeSuccess, "Launched: "+msg.Item.Title)
	}
	change := msg.Change
	var cmds []tea.Cmd
	for _, r := range change.Resources {
		if _, tracked := m.collections[r.Key()]; !tracked && r.Kind != catalog.Playlists {
			continue
		}
		if r.LibraryID != "" && m.findLibrary(r.LibraryID) == nil {
			continue
		}
		// Reconciled snapshots arrive as published state. Collections the write
		// may have changed on the server revalidate if anything shows them.
		background := libraryStateID(r) != ""
		visible := false
		for i := 0; i < m.columns.Len(); i++ {
			visible = visible || m.columns.Get(i).ContentID() == r.Key()
		}
		if background || visible {
			cmds = append(cmds, m.loadResource(r, catalog.Revalidate, background))
		}
	}

	if change.Applied && change.Mutation.Kind == catalog.MutationDeletePlaylist {
		if r, ok := m.topResource(); ok && r.Kind == catalog.PlaylistItems && r.ID == change.Mutation.PlaylistID {
			cmds = append(cmds, m.handleBack())
		}
	}
	if msg.Err != nil {
		cmds = append(cmds, m.notifyError("Update failed", msg.Err))
	} else {
		text := "Playlist updated"
		if change.Mutation.Kind == catalog.MutationWatch {
			text = "Marked unwatched: " + change.Mutation.Title
			if change.Mutation.Played {
				text = "Marked watched: " + change.Mutation.Title
			}
		}
		if change.Mutation.Kind == catalog.MutationCreatePlaylist {
			text = "Created playlist: " + change.Mutation.Title
		}
		if change.Mutation.Kind == catalog.MutationDeletePlaylist {
			text = "Playlist deleted"
		}
		cmds = append(cmds, m.notify(noticeSuccess, text))
	}
	if change.Warning != nil {
		cmds = append(cmds, m.notifyError("Server updated; local cache needs refresh", change.Warning))
	}
	return tea.Batch(cmds...)
}

func (m *Model) notifyError(scope string, err error) tea.Cmd {
	if err == nil || errors.Is(err, context.Canceled) {
		return nil
	}
	if errors.Is(err, domain.ErrAuthFailed) {
		return m.notify(noticeAlert, authFailedStatusMsg)
	}
	return m.notify(noticeError, fmt.Sprintf("%s: %v", scope, err))
}
func (m *Model) activeSyncCount() int {
	n := 0
	for _, state := range m.libraryStates {
		if state.Activity.Visible {
			n++
		}
	}
	return n
}
func (m *Model) libraryColumn() *components.ListColumn { return m.columns.Get(0) }
func (m *Model) updateLibraryStates() {
	if col := m.libraryColumn(); col != nil {
		col.SetLibraryStates(m.libraryStates)
	}
	m.inspector.SetLibraryStates(m.libraryStates)
}
func (m *Model) findLibrary(id string) *domain.Library {
	for _, lib := range m.libraries {
		if lib.ID == id {
			return &lib
		}
	}
	return nil
}
func (m *Model) updateInspector() {
	if col := m.columns.Top(); col != nil {
		m.inspector.SetItem(col.SelectedItem())
	} else {
		m.inspector.SetItem(nil)
	}
}
func (m *Model) resourceName(r catalog.Resource) string {
	if r.Kind == catalog.Libraries {
		return "libraries"
	}
	if r.Kind == catalog.Playlists {
		return "playlists"
	}
	for i := 0; i < m.columns.Len(); i++ {
		if col := m.columns.Get(i); col.ContentID() == r.Key() {
			return col.Title()
		}
	}
	if lib := m.findLibrary(r.LibraryID); lib != nil {
		return lib.Name
	}
	return "items"
}

// pruneLibraryRequests detaches removed libraries after an authoritative root
// refresh. Queued results cannot recreate removed rows or keep sync active.
func (m *Model) pruneLibraryRequests() {
	allowed := make(map[string]bool, len(m.libraries))
	for _, lib := range m.libraries {
		allowed[lib.ID] = true
	}
	for key, st := range m.collections {
		r := st.Resource
		if r.LibraryID == "" || allowed[r.LibraryID] {
			continue
		}
		m.requests.stop(viewOwner(r))
		m.requests.stop(syncOwner(r))
		delete(m.collections, key)
		delete(m.indicators, key)
		delete(m.libraryStates, r.LibraryID)
	}
	m.updateLibraryStates()
}
