package tui

import (
	"context"
	"errors"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mmcdole/kino/internal/catalog"
	"github.com/mmcdole/kino/internal/config"
	"github.com/mmcdole/kino/internal/domain"
	"github.com/mmcdole/kino/internal/search"
	"github.com/mmcdole/kino/internal/tui/components"
)

// authFailedStatusMsg tells the user how to recover from a revoked/expired
// token. Shown persistently (not auto-cleared) since action is required.
const authFailedStatusMsg = "Session expired or revoked — press L to log out, then run kino to sign in again"

// ApplicationState represents the current state of the application
type ApplicationState int

const (
	StateBrowsing ApplicationState = iota
	StateHelp
	StateConfirmLogout
	StateConfirmDeletePlaylist
)

// Layout proportions for Miller Columns
const (
	// 3-Column Smart Ratios (Inspector visible)
	ParentColumnPercent3   = 25 // Parent context
	ActiveColumnPercent3   = 35 // Active/focused
	InspectorColumnPercent = 30 // Inspector (summary)

	// 3-Column Focus Mode (Inspector hidden) - show more navigation context
	GrandparentColumnPercent = 25 // Grandparent context
	ParentColumnPercent2     = 30 // Parent context
	ActiveColumnPercent2     = 45 // Active/focused

	// Root level (single column + inspector)
	RootColumnPercent    = 40
	RootInspectorPercent = 60

	MinColumnWidth = 15

	// Vertical layout: single footer line
	ChromeHeight = 1

	// Synthetic library entry for playlists
	playlistsLibraryID = "__playlists__"
)

// playlistsLibraryEntry returns the synthetic library entry for playlists
func playlistsLibraryEntry() domain.Library {
	return domain.Library{
		ID:   playlistsLibraryID,
		Name: "Playlists",
		Type: "playlist",
	}
}

// allLibraryEntries returns libraries plus the synthetic Playlists entry
func (m *Model) allLibraryEntries() []domain.Library {
	return append(m.Libraries, playlistsLibraryEntry())
}

type Model struct {
	State         ApplicationState
	Ready         bool
	Width, Height int
	SpinnerFrame  int
	ShowInspector bool
	LoggedOut     bool
	loggingOut    bool

	Catalog     Catalog
	PlaybackSvc Playback
	SearchIndex *search.Index
	UIConfig    config.UIConfig

	ColumnStack   *ColumnStack
	Inspector     components.Inspector
	GlobalSearch  components.GlobalSearch
	SortModal     components.SortModal
	PlaylistModal components.PlaylistModal
	InputModal    components.InputModal

	Libraries     []domain.Library
	LibraryStates map[string]components.CollectionFeedback
	collections   map[string]*collectionState
	requests      *requests

	notice                    Notice
	noticeSeq                 int
	searchSeq                 uint64
	navPlan                   *NavPlan
	pendingDeletePlaylistID   string
	pendingDeletePlaylistName string
}

func NewModel(ctx context.Context, svc Catalog, playback Playback, index *search.Index, ui config.UIConfig) Model {
	m := Model{
		Catalog: svc, PlaybackSvc: playback, SearchIndex: index, UIConfig: ui,
		ColumnStack:   NewColumnStack(),
		Inspector:     components.NewInspector(),
		GlobalSearch:  components.NewGlobalSearch(),
		PlaylistModal: components.NewPlaylistModal(),
		InputModal:    components.NewInputModal(),
		LibraryStates: make(map[string]components.CollectionFeedback),
		requests:      newRequests(ctx),
		collections:   make(map[string]*collectionState),
	}
	root := catalog.Resource{Kind: catalog.Libraries}
	col := components.NewListColumn(components.ColumnTypeLibraries, "Libraries")
	col.SetContentID(root.Key())
	col.SetFeedback(components.CollectionFeedback{Pending: true})
	col.SetShowWatchStatus(ui.ShowWatchStatus)
	col.SetShowLibraryCounts(ui.ShowLibraryCounts)
	m.ColumnStack.Reset(col)
	m.collection(root)
	return m
}
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.loadResource(catalog.Resource{Kind: catalog.Libraries}, catalog.Revalidate, false), TickCmd(100*time.Millisecond))
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.update(msg)
	model := next.(Model)
	model.updateInspector()
	return model, cmd
}

func (m Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.loggingOut {
		if result, ok := msg.(LogoutCompleteMsg); ok {
			if result.Error != nil {
				m.loggingOut = false
				m.State = StateBrowsing
				return m, m.notifyError("Logout failed", result.Error)
			}
			m.LoggedOut = true
			m.requests.cancel()
			return m, tea.Quit
		}
		return m, nil
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width, m.Height, m.Ready = msg.Width, msg.Height, true
		m.updateLayout()
		return m, nil
	case tea.KeyMsg:
		return m.handleKeyMsg(msg)
	case TickMsg:
		m.SpinnerFrame++
		m.ColumnStack.UpdateSpinnerFrame(m.SpinnerFrame)
		return m, TickCmd(100 * time.Millisecond)
	case ResourceMsg:
		return m.handleResource(msg)
	case ActionMsg:
		return m.handleAction(msg)
	case PlaylistModalDataMsg:
		if !m.requests.owns(msg.Request) {
			return m, nil
		}
		m.requests.finish(msg.Request)
		if msg.Err != nil {
			m.PlaylistModal.Hide()
			return m, m.notifyError("Loading playlists", msg.Err)
		}
		m.PlaylistModal.Show(msg.Membership.Playlists, msg.Membership.Present, &msg.Item)
		m.PlaylistModal.SetSize(m.Width, m.Height)
		return m, nil
	case ClearNoticeMsg:
		m.expireNotice(msg.Seq)
		return m, nil
	case ShowLoadingMsg:
		if m.requests.owns(msg.Request) {
			req := m.requests.active[msg.Request.Owner]
			if req.Network {
				req.IndicatorVisible = true
				m.requests.active[req.Owner] = req
				m.updateResourceFeedback(req.Resource)
			}
		}
		return m, nil
	case SearchDebounceMsg:
		if !m.GlobalSearch.IsVisible() || msg.Seq != m.searchSeq {
			return m, nil
		}
		req := m.requests.begin("search", catalog.Resource{}, catalog.Browse)
		libraries := append([]domain.Library(nil), m.Libraries...)
		return m, func() tea.Msg {
			return SearchResultsMsg{Request: req, Results: m.SearchIndex.Search(req.ctx, msg.Query, libraries)}
		}
	case ShowSearchLoadingMsg:
		if m.GlobalSearch.IsVisible() && msg.Seq == m.searchSeq {
			m.GlobalSearch.ShowLoading()
		}
		return m, nil
	case SearchResultsMsg:
		if !m.requests.owns(msg.Request) || !m.GlobalSearch.IsVisible() {
			return m, nil
		}
		m.requests.finish(msg.Request)
		m.GlobalSearch.SetResults(msg.Results)
		return m, nil
	case SearchIndexChangedMsg:
		if m.GlobalSearch.IsVisible() {
			return m, m.scheduleSearch()
		}
		return m, nil
	}
	// Bubble Tea text-input cursor messages belong to the active modal too.
	if m.GlobalSearch.IsVisible() {
		var cmd tea.Cmd
		m.GlobalSearch, cmd, _ = m.GlobalSearch.Update(msg)
		return m, cmd
	}
	if m.InputModal.IsVisible() {
		var cmd tea.Cmd
		m.InputModal, cmd, _ = m.InputModal.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m Model) handleResource(msg ResourceMsg) (tea.Model, tea.Cmd) {
	if !m.requests.owns(msg.Request) {
		return m, nil
	}
	r := msg.Request.Resource
	var cmds []tea.Cmd
	if msg.Next != nil {
		cmds = append(cmds, msg.Next)
	}
	if msg.Stage == loadNetwork {
		req := m.requests.active[msg.Request.Owner]
		if !req.Network {
			req.Network = true
			m.requests.active[req.Owner] = req
			cmds = append(cmds, showLoadingCmd(req))
		}
		return m, tea.Batch(cmds...)
	}
	if msg.Stage == loadProgress {
		// Activity belongs to the live subscription. A cached observation may
		// advance the snapshot revision while this same request keeps fetching.
		req := m.requests.active[msg.Request.Owner]
		req.Progress = msg.Progress
		m.requests.active[req.Owner] = req
		m.updateResourceFeedback(r)
		return m, tea.Batch(cmds...)
	}
	state := m.collection(r)
	hasSnapshot := msg.Snapshot.FromCache || msg.Err == nil
	accepted := hasSnapshot && state.accepts(msg.Snapshot)
	// A failed attempt has its own ordering, independent of the fallback payload.
	obsolete := !accepted && m.requests.active[msg.Request.Owner].Revision < state.RequiredRevision
	if accepted {
		if msg.Stage == loadCached {
			req := m.requests.active[msg.Request.Owner]
			req.Revision = msg.Snapshot.Revision
			m.requests.active[req.Owner] = req
		}
		cmds = append(cmds, m.applySnapshot(msg.Snapshot))
		if r.Kind == catalog.Libraries {
			if msg.Stage == loadFinished && msg.Err == nil && msg.Snapshot.Validated {
				m.pruneLibraryRequests()
			}
			policy := catalog.Revalidate
			if msg.Request.Policy == catalog.Refresh {
				policy = catalog.Refresh
			}
			for _, lib := range m.Libraries {
				resource := catalog.LibraryResource(lib)
				child := m.collection(resource)
				if child.BackgroundRequest != msg.Request.ID || child.Resource.Version != resource.Version {
					child.BackgroundRequest = msg.Request.ID
					cmds = append(cmds, m.loadResource(resource, policy, true))
				}
			}
			playlists := catalog.Resource{Kind: catalog.Playlists}
			child := m.collection(playlists)
			if child.BackgroundRequest != msg.Request.ID {
				child.BackgroundRequest = msg.Request.ID
				cmds = append(cmds, m.loadResource(playlists, policy, true))
			}
		}
		if msg.Stage == loadFinished && msg.Err == nil && msg.Snapshot.Validated {
			m.pruneNavigation()
		}
	}

	if msg.Stage == loadFinished {
		m.requests.finish(msg.Request)
		// A queued pre-mutation completion may be the last subscriber. Recover
		// once at the new revision; a current failed attempt remains retryable.
		if obsolete && (!state.Known || state.Snapshot.Revision < state.RequiredRevision) {
			cmds = append(cmds, m.loadResource(r, catalog.Browse, libraryStateID(r) != ""))
		}
		if !obsolete {
			if msg.Err != nil && !errors.Is(msg.Err, context.Canceled) {
				state.Error = msg.Err
			} else if msg.Err == nil && msg.Snapshot.Validated {
				state.Error = nil
			}
		}
		if !obsolete && msg.Err != nil {
			if m.navPlan != nil && m.navPlan.AwaitKey == r.Key() {
				m.clearNavPlan()
			}
			cmds = append(cmds, m.notifyError("Loading "+m.resourceName(r), msg.Err))
		}
		if !obsolete && msg.Snapshot.Warning != nil {
			cmds = append(cmds, m.notifyError("Loaded "+m.resourceName(r), msg.Snapshot.Warning))
		}
	}
	m.updateResourceFeedback(r)
	if accepted && msg.Err == nil {
		cmds = append(cmds, m.advanceNavPlanAfterLoad(r.Key(), msg.Stage == loadFinished))
	}
	return m, tea.Batch(cmds...)
}

func (m Model) handleAction(msg ActionMsg) (tea.Model, tea.Cmd) {
	if !m.requests.owns(msg.Request) {
		return m, nil
	}
	m.requests.finish(msg.Request)
	if msg.Playback {
		if msg.Err != nil {
			return m, m.notifyError("Starting playback", msg.Err)
		}
		return m, m.notify(NoticeSuccess, "Launched: "+msg.Item.Title)
	}
	change := msg.Change
	for key, revision := range change.Revisions {
		if state := m.collections[key]; state != nil {
			state.RequiredRevision = max(state.RequiredRevision, revision)
		}
	}
	var cmds []tea.Cmd
	for _, snapshot := range change.Snapshots {
		// Closed collections can still feed search and later navigation.
		if state := m.collections[snapshot.Resource.Key()]; state != nil && state.accepts(snapshot) {
			cmds = append(cmds, m.applySnapshot(snapshot))
			cmds = append(cmds, m.advanceNavPlanAfterLoad(snapshot.Resource.Key(), false))
			m.updateResourceFeedback(snapshot.Resource)
		}
	}
	for _, r := range change.Resources {
		if m.collections[r.Key()] == nil && r.Kind != catalog.Playlists {
			continue
		}
		// Restart subscriptions whose result may already be queued. New callers
		// still join shared catalog work; a stale UI request cannot swallow recovery.
		m.requests.stop(viewOwner(r))
		m.requests.stop(syncOwner(r))
		background := libraryStateID(r) != ""
		visible := false
		for i := 0; i < m.ColumnStack.Len(); i++ {
			visible = visible || m.ColumnStack.Get(i).ContentID() == r.Key()
		}
		if background || visible {
			cmds = append(cmds, m.loadResource(r, catalog.Revalidate, background))
		}
	}

	if change.Applied && change.Mutation.Kind == catalog.DeletePlaylist {
		if r, ok := m.topResource(); ok && r.Kind == catalog.PlaylistItems && r.ID == change.Mutation.PlaylistID {
			model, cmd := m.handleBack()
			m = model.(Model)
			cmds = append(cmds, cmd)
		}
	}
	if msg.Err != nil {
		cmds = append(cmds, m.notifyError("Update failed", msg.Err))
	} else {
		text := "Playlist updated"
		if change.Mutation.Kind == catalog.Watch {
			text = "Marked unwatched: " + change.Mutation.Title
			if change.Mutation.Played {
				text = "Marked watched: " + change.Mutation.Title
			}
		}
		if change.Mutation.Kind == catalog.CreatePlaylist {
			text = "Created playlist: " + change.Mutation.Title
		}
		if change.Mutation.Kind == catalog.DeletePlaylist {
			text = "Playlist deleted"
		}
		cmds = append(cmds, m.notify(NoticeSuccess, text))
	}
	if change.Warning != nil {
		cmds = append(cmds, m.notifyError("Server updated; local cache needs refresh", change.Warning))
	}
	return m, tea.Batch(cmds...)
}

func (m *Model) notifyError(scope string, err error) tea.Cmd {
	if err == nil || errors.Is(err, context.Canceled) {
		return nil
	}
	if errors.Is(err, domain.ErrAuthFailed) {
		return m.notify(NoticeAlert, authFailedStatusMsg)
	}
	return m.notify(NoticeError, fmt.Sprintf("%s: %v", scope, err))
}
func (m Model) activeSyncCount() int {
	n := 0
	for _, state := range m.LibraryStates {
		if state.Activity.Visible {
			n++
		}
	}
	return n
}
func (m *Model) libraryColumn() *components.ListColumn { return m.ColumnStack.Get(0) }
func (m *Model) updateLibraryStates() {
	if col := m.libraryColumn(); col != nil {
		col.SetLibraryStates(m.LibraryStates)
	}
	m.Inspector.SetLibraryStates(m.LibraryStates)
}
func (m Model) findLibrary(id string) *domain.Library {
	for _, lib := range m.Libraries {
		if lib.ID == id {
			return &lib
		}
	}
	return nil
}
func (m *Model) updateInspector() {
	if col := m.ColumnStack.Top(); col != nil {
		m.Inspector.SetItem(col.SelectedItem())
	} else {
		m.Inspector.SetItem(nil)
	}
}
func (m Model) resourceName(r catalog.Resource) string {
	if r.Kind == catalog.Libraries {
		return "libraries"
	}
	if r.Kind == catalog.Playlists {
		return "playlists"
	}
	for i := 0; i < m.ColumnStack.Len(); i++ {
		if col := m.ColumnStack.Get(i); col.ContentID() == r.Key() {
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
	allowed := make(map[string]bool, len(m.Libraries))
	for _, lib := range m.Libraries {
		allowed[lib.ID] = true
	}
	for key, state := range m.collections {
		r := state.Resource
		if r.LibraryID == "" || allowed[r.LibraryID] {
			continue
		}
		m.requests.stop(viewOwner(r))
		m.requests.stop(syncOwner(r))
		delete(m.collections, key)
		delete(m.LibraryStates, r.LibraryID)
	}
	m.updateLibraryStates()
}
