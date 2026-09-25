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
	overlay       overlay
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
	collections   map[string]catalog.State
	indicators    map[string]uint64 // key → server attempt whose spinner is due
	requests      *requests

	notice                    Notice
	noticeSeq                 int
	searchSeq                 uint64
	navPlan       *NavPlan
	confirmDelete *domain.Playlist // the playlist overlayConfirmDelete asks about
}

func NewModel(ctx context.Context, svc Catalog, playback Playback, index *search.Index, ui config.UIConfig) *Model {
	m := &Model{
		Catalog: svc, PlaybackSvc: playback, SearchIndex: index, UIConfig: ui,
		ColumnStack:   NewColumnStack(),
		Inspector:     components.NewInspector(),
		GlobalSearch:  components.NewGlobalSearch(),
		PlaylistModal: components.NewPlaylistModal(),
		InputModal:    components.NewInputModal(),
		LibraryStates: make(map[string]components.CollectionFeedback),
		requests:      newRequests(ctx),
		collections:   make(map[string]catalog.State),
		indicators:    make(map[string]uint64),
	}
	root := catalog.Resource{Kind: catalog.Libraries}
	col := components.NewListColumn("Libraries", components.ColumnOptions{})
	col.SetContentID(root.Key())
	col.SetFeedback(components.CollectionFeedback{Pending: true})
	col.SetShowWatchStatus(ui.ShowWatchStatus)
	col.SetShowLibraryCounts(ui.ShowLibraryCounts)
	m.ColumnStack.Reset(col)
	m.track(root)
	return m
}
func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		listen(m.Catalog, m.requests.ctx),
		m.loadResource(catalog.Resource{Kind: catalog.Libraries}, catalog.Revalidate, false),
		TickCmd(100*time.Millisecond),
	)
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmd := m.update(msg)
	m.updateInspector()
	return m, cmd
}

func (m *Model) update(msg tea.Msg) tea.Cmd {
	if m.loggingOut {
		if result, ok := msg.(LogoutCompleteMsg); ok {
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
		m.Width, m.Height, m.Ready = msg.Width, msg.Height, true
		m.updateLayout()
		return nil
	case tea.KeyMsg:
		return m.handleKeyMsg(msg)
	case TickMsg:
		m.SpinnerFrame++
		m.ColumnStack.UpdateSpinnerFrame(m.SpinnerFrame)
		return TickCmd(100 * time.Millisecond)
	case StatesMsg:
		var cmds []tea.Cmd
		for _, st := range msg {
			cmds = append(cmds, m.applyState(st))
		}
		return tea.Batch(append(cmds, listen(m.Catalog, m.requests.ctx))...)
	case LoadDoneMsg:
		return m.handleLoadDone(msg)
	case ActionMsg:
		return m.handleAction(msg)
	case PlaylistModalDataMsg:
		if !m.requests.owns(msg.Request) {
			return nil
		}
		m.requests.finish(msg.Request)
		if msg.Err != nil {
			m.overlay = overlayNone
			return m.notifyError("Loading playlists", msg.Err)
		}
		m.PlaylistModal.Show(msg.Membership.Playlists, msg.Membership.Present, &msg.Item)
		m.PlaylistModal.SetSize(m.Width, m.Height)
		return nil
	case ClearNoticeMsg:
		m.expireNotice(msg.Seq)
		return nil
	case ShowLoadingMsg:
		if st := m.collections[msg.Key]; st.Fetching && st.Attempt == msg.Attempt {
			m.indicators[msg.Key] = msg.Attempt
			m.updateResourceFeedback(st.Resource)
		}
		return nil
	case SearchDebounceMsg:
		if m.overlay != overlaySearch || msg.Seq != m.searchSeq {
			return nil
		}
		req := m.requests.begin("search", catalog.Resource{}, catalog.Browse)
		libraries := append([]domain.Library(nil), m.Libraries...)
		return func() tea.Msg {
			return SearchResultsMsg{Request: req, Results: m.SearchIndex.Search(req.ctx, msg.Query, libraries)}
		}
	case ShowSearchLoadingMsg:
		if m.overlay == overlaySearch && msg.Seq == m.searchSeq {
			m.GlobalSearch.ShowLoading()
		}
		return nil
	case SearchResultsMsg:
		if !m.requests.owns(msg.Request) || m.overlay != overlaySearch {
			return nil
		}
		m.requests.finish(msg.Request)
		m.GlobalSearch.SetResults(msg.Results)
		return nil
	case SearchIndexChangedMsg:
		if m.overlay == overlaySearch {
			return m.scheduleSearch()
		}
		return nil
	}
	// Bubble Tea text-input cursor messages belong to the open overlay too.
	var cmd tea.Cmd
	switch m.overlay {
	case overlaySearch:
		m.GlobalSearch, cmd, _ = m.GlobalSearch.Update(msg)
	case overlayInput:
		m.InputModal, cmd, _ = m.InputModal.Update(msg)
	}
	return cmd
}

// handleLoadDone reports the outcome of this model's own request. Content
// and loading state come from the catalog's published state instead.
func (m *Model) handleLoadDone(msg LoadDoneMsg) tea.Cmd {
	if !m.requests.owns(msg.Request) {
		return nil
	}
	m.requests.finish(msg.Request)
	r := msg.Request.Resource
	var cmds []tea.Cmd
	if msg.Err != nil {
		if m.navPlan != nil && m.navPlan.AwaitKey == r.Key() {
			m.clearNavPlan()
		}
		cmds = append(cmds, m.notifyError("Loading "+m.resourceName(r), msg.Err))
	}
	if msg.Warning != nil {
		cmds = append(cmds, m.notifyError("Loaded "+m.resourceName(r), msg.Warning))
	}
	m.updateResourceFeedback(r)
	return tea.Batch(cmds...)
}

func (m *Model) handleAction(msg ActionMsg) tea.Cmd {
	if !m.requests.owns(msg.Request) {
		return nil
	}
	m.requests.finish(msg.Request)
	if msg.Playback {
		if msg.Err != nil {
			return m.notifyError("Starting playback", msg.Err)
		}
		return m.notify(NoticeSuccess, "Launched: "+msg.Item.Title)
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
		for i := 0; i < m.ColumnStack.Len(); i++ {
			visible = visible || m.ColumnStack.Get(i).ContentID() == r.Key()
		}
		if background || visible {
			cmds = append(cmds, m.loadResource(r, catalog.Revalidate, background))
		}
	}

	if change.Applied && change.Mutation.Kind == catalog.DeletePlaylist {
		if r, ok := m.topResource(); ok && r.Kind == catalog.PlaylistItems && r.ID == change.Mutation.PlaylistID {
			cmds = append(cmds, m.handleBack())
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
	return tea.Batch(cmds...)
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
func (m *Model) activeSyncCount() int {
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
func (m *Model) findLibrary(id string) *domain.Library {
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
func (m *Model) resourceName(r catalog.Resource) string {
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
	for key, st := range m.collections {
		r := st.Resource
		if r.LibraryID == "" || allowed[r.LibraryID] {
			continue
		}
		m.requests.stop(viewOwner(r))
		m.requests.stop(syncOwner(r))
		delete(m.collections, key)
		delete(m.indicators, key)
		delete(m.LibraryStates, r.LibraryID)
	}
	m.updateLibraryStates()
}
