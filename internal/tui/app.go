package tui

import (
	"context"
	"errors"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mmcdole/kino/internal/catalog"
	"github.com/mmcdole/kino/internal/config"
	"github.com/mmcdole/kino/internal/domain"
	"github.com/mmcdole/kino/internal/search"
	"github.com/mmcdole/kino/internal/tui/components"
	"time"
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
	State                                              ApplicationState
	Ready                                              bool
	Catalog                                            Catalog
	PlaybackSvc                                        Playback
	SearchIndex                                        *search.Index
	ColumnStack                                        *ColumnStack
	Inspector                                          components.Inspector
	GlobalSearch                                       components.GlobalSearch
	SortModal                                          components.SortModal
	PlaylistModal                                      components.PlaylistModal
	InputModal                                         components.InputModal
	Libraries                                          []domain.Library
	Width, Height                                      int
	SpinnerFrame                                       int
	ShowInspector                                      bool
	notice                                             Notice
	noticeSeq                                          int
	LibraryStates                                      map[string]components.LibrarySyncState
	navPlan                                            *NavPlan
	pendingDeletePlaylistID, pendingDeletePlaylistName string
	UIConfig                                           config.UIConfig
	requests                                           *requests
	resources                                          map[string]catalog.Resource
	revisions                                          map[string]uint64
	loaded                                             map[string]bool
	backgroundStarted                                  map[string]uint64
	searchSeq                                          uint64
	LoggedOut                                          bool
	loggingOut                                         bool
}

func NewModel(ctx context.Context, svc Catalog, playback Playback, index *search.Index, ui config.UIConfig) Model {
	m := Model{Catalog: svc, PlaybackSvc: playback, SearchIndex: index, UIConfig: ui,
		ColumnStack: NewColumnStack(), Inspector: components.NewInspector(), GlobalSearch: components.NewGlobalSearch(),
		PlaylistModal: components.NewPlaylistModal(), InputModal: components.NewInputModal(),
		LibraryStates: make(map[string]components.LibrarySyncState), requests: newRequests(ctx),
		resources: make(map[string]catalog.Resource), revisions: make(map[string]uint64), backgroundStarted: make(map[string]uint64)}
	root := catalog.Resource{Kind: catalog.Libraries}
	col := components.NewListColumn(components.ColumnTypeLibraries, "Libraries")
	col.SetContentID(root.Key())
	col.SetLoading(true)
	col.SetShowWatchStatus(ui.ShowWatchStatus)
	col.SetShowLibraryCounts(ui.ShowLibraryCounts)
	m.ColumnStack.Reset(col)
	m.resources[root.Key()] = root
	return m
}
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.loadResource(catalog.Resource{Kind: catalog.Libraries}, catalog.Revalidate, false), TickCmd(100*time.Millisecond))
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
	case ClearLibraryStatusMsg:
		for key, r := range m.resources {
			if libraryStateID(r) == msg.LibraryID && m.revisions[key] == msg.Revision {
				state := m.LibraryStates[msg.LibraryID]
				if state.Status == components.StatusSynced {
					state.Status = components.StatusIdle
					m.LibraryStates[msg.LibraryID] = state
					m.updateLibraryStates()
				}
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
	if msg.Stage == loadProgress {
		if id := libraryStateID(r); id != "" {
			state := m.LibraryStates[id]
			state.Loaded = msg.Progress.Loaded
			state.Total = msg.Progress.Total
			m.LibraryStates[id] = state
			m.updateLibraryStates()
		}
		return m, tea.Batch(cmds...)
	}
	hasSnapshot := !msg.Snapshot.FetchedAt.IsZero() || msg.Snapshot.FromCache || msg.Err == nil
	accepted := hasSnapshot && msg.Snapshot.Revision >= m.revisions[r.Key()]
	if accepted {
		m.revisions[r.Key()] = msg.Snapshot.Revision
		if r.Kind == catalog.Libraries {
			m.Libraries = nil
			for _, item := range msg.Snapshot.Items {
				if lib, ok := item.(*domain.Library); ok {
					m.Libraries = append(m.Libraries, *lib)
				}
			}
			m.libraryColumn().ReplaceItems(components.WrapLibraries(m.allLibraryEntries()))

			policy := catalog.Revalidate
			if msg.Request.Policy == catalog.Refresh {
				policy = catalog.Refresh
			}
			for _, lib := range m.Libraries {
				resource := catalog.LibraryResource(lib)
				previous, exists := m.resources[resource.Key()]
				if m.backgroundStarted[resource.Key()] != msg.Request.ID || (exists && previous.Version != resource.Version) {
					m.backgroundStarted[resource.Key()] = msg.Request.ID
					cmds = append(cmds, m.loadResource(resource, policy, true))
				}
			}
			playlists := catalog.Resource{Kind: catalog.Playlists}
			if m.backgroundStarted[playlists.Key()] != msg.Request.ID {
				m.backgroundStarted[playlists.Key()] = msg.Request.ID
				cmds = append(cmds, m.loadResource(playlists, policy, true))
			}
		} else {
			for i := 0; i < m.ColumnStack.Len(); i++ {
				if col := m.ColumnStack.Get(i); col.ContentID() == r.Key() {
					col.ReplaceItems(domain.CloneItems(msg.Snapshot.Items))
				}
			}
		}
		if r.Kind == catalog.Movies || r.Kind == catalog.Shows || r.Kind == catalog.Mixed {
			snapshot := msg.Snapshot.Clone()
			cmds = append(cmds, func() tea.Msg {
				m.SearchIndex.ReplaceLibrary(r.LibraryID, snapshot.Revision, snapshot.Items)
				return SearchIndexChangedMsg{}
			})
		}
		m.updateInspector()
		if msg.Stage == loadFinished && msg.Err == nil {
			m.pruneNavigation()
		}
	}
	if msg.Stage == loadCached {
		for i := 0; i < m.ColumnStack.Len(); i++ {
			if col := m.ColumnStack.Get(i); col.ContentID() == r.Key() {
				col.SetLoading(false)
				col.SetRefreshing(true)
			}
		}
	} else {
		m.requests.finish(msg.Request)
		// A second subscriber may still be refreshing this collection.
		_, viewPending := m.requests.active[viewOwner(r)]
		_, syncPending := m.requests.active[syncOwner(r)]
		for i := 0; i < m.ColumnStack.Len(); i++ {
			col := m.ColumnStack.Get(i)
			if col.ContentID() != r.Key() {
				continue
			}
			col.SetRefreshing(viewPending || syncPending)
			if msg.Err != nil && !col.HasContent() {
				col.SetLoadFailed()
			}
		}
		if id := libraryStateID(r); id != "" {
			state := m.LibraryStates[id]
			state.Loaded = len(msg.Snapshot.Items)
			state.Total = state.Loaded
			state.FromCache = msg.Snapshot.FromCache
			state.Error = msg.Err
			switch {
			case viewPending || syncPending:
				state.Status = components.StatusSyncing
			case msg.Err != nil:
				state.Status = components.StatusError
			default:
				state.Status = components.StatusSynced
				cmds = append(cmds, m.clearLibraryStatus(id, msg.Snapshot.Revision))
			}
			m.LibraryStates[id] = state
			m.updateLibraryStates()
		}
		if msg.Err != nil {
			if m.navPlan != nil && m.navPlan.AwaitKey == r.Key() {
				m.clearNavPlan()
			}
			cmds = append(cmds, m.notifyError("Loading "+m.resourceName(r), msg.Err))
		}
		if msg.Snapshot.Warning != nil {
			cmds = append(cmds, m.notifyError("Loaded "+m.resourceName(r), msg.Snapshot.Warning))
		}
	}
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
		if revision > m.revisions[key] {
			m.revisions[key] = revision
		}
	}
	var cmds []tea.Cmd
	if change.Applied && change.Mutation.Kind == catalog.Watch {
		m.applyWatchState(change.Mutation.ItemID, change.Mutation.Played)
	}
	for _, r := range change.Resources {
		// Reconcile all affected open projections; root playlists also updates its
		// parent list/count after an item mutation.
		for i := 0; i < m.ColumnStack.Len(); i++ {
			if m.ColumnStack.Get(i).ContentID() == r.Key() {
				cmds = append(cmds, m.loadResource(r, catalog.Revalidate, false))
				break
			}
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
		if state.Status == components.StatusSyncing {
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

func (m *Model) applyWatchState(itemID string, played bool) {

	// Patch the item wherever a column renders it, and adjust unwatched
	// counters on visible show/season rows if an episode flipped state.
	var patched *domain.MediaItem
	flipped := false
	for i := 0; i < m.ColumnStack.Len(); i++ {
		if col := m.ColumnStack.Get(i); col != nil {
			if item, f := col.ApplyWatchState(itemID, played); item != nil {
				patched = item
				flipped = flipped || f
			}
		}
	}

	if flipped && patched != nil && patched.ShowID != "" {
		delta := 1
		if played {
			delta = -1
		}
		for i := 0; i < m.ColumnStack.Len(); i++ {
			if col := m.ColumnStack.Get(i); col != nil {
				col.AdjustUnwatchedCounts(patched.ShowID, patched.ParentID, delta)
			}
		}
	}

	m.updateInspector()
}
