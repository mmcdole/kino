# Kino Design Review — July 2026

Full-codebase review (~12.6k lines Go) covering architecture, separation of concerns,
consistency, UX, and security. Four review passes: TUI architecture, UX consistency,
services/domain/store, backends/config/cross-cutting. Every finding was verified
against source with file:line references; the two most severe were independently
re-confirmed.

## Themes

1. **No shared transport per backend.** Each backend has a good `doRequest` helper,
   but half the methods hand-roll their own HTTP. Every backend consistency bug below
   (401 mapping, retry asymmetry, header drift, missing Accept) is a symptom.
2. **`SetItems` resets the world.** The single biggest UX defect class (cursor, sort,
   and filter loss across five different user flows) traces to one function
   unconditionally resetting view state (`list_column.go:316-370`).
3. **Cache invalidation is all-or-nothing.** `refreshCurrentView` starts with
   `InvalidateAll()`; marking one episode watched dumps every cache in the app.
4. **Two data paths into the TUI.** The TUI reads the Store directly at 11 sites and
   calls services at others, with different staleness semantics; cache policy is
   smeared across the UI layer.
5. **`interface{}` joints.** `SetItems(interface{})`, `getCached func() interface{}`,
   and columnType-driven type switches fight the `domain.ListItem` interface that
   already exists for this purpose.
6. **Token hygiene.** The auth token is written 0644, embedded in `ps`-visible player
   args, and logged at Debug.

---

## A. Correctness bugs (live defects)

### A1. Jellyfin playlist-remove silently no-ops — wrong ID type   `HIGH`
`jellyfin/client.go:627-628` sends the media item ID as `EntryIds`, but Jellyfin's
`DELETE /Playlists/{id}/Items` expects per-entry `PlaylistItemId` GUIDs. The Jellyfin
`Item` DTO doesn't even have the field (`jellyfin/dto.go`). Jellyfin returns 204 for
unmatched IDs, so the UI reports success while the playlist is unchanged. This is the
same bug class fixed for Plex in bfc8685 (`plex/client.go:543-571` resolves
`playlistItemID` first); Jellyfin never got the fix.
**Fix:** request `PlaylistItemId` in `GetPlaylistItems`, resolve item→entry like Plex.

### A2. 401 unmapped on all hand-rolled request paths — breaks the re-auth prompt   `HIGH`
Only `doRequest` maps 401 → `domain.ErrAuthFailed`. Hand-rolled paths return generic
`status %d` errors: Jellyfin `MarkPlayed/MarkUnplayed/CreatePlaylist/AddToPlaylist/
RemoveFromPlaylist/DeletePlaylist` (`jellyfin/client.go:452,476,572,618,645,669`),
Plex `CreatePlaylist/AddToPlaylist/RemoveFromPlaylist(DELETE)/DeletePlaylist`
(`plex/client.go:474,534,593,624`). The v0.4.2 "session expired — press L" UX is keyed
on `errors.Is(err, domain.ErrAuthFailed)` (`tui/app.go:363,392`), so a revoked token
during any mutation shows a cryptic transient error instead.
**Fix:** one request helper per backend (method/body/allowed-status); all error
mapping in one place. Kills ~120 lines of duplicated header/boilerplate too.

### A3. Mark-watched nukes every cache; in playlists it resets the whole app   `HIGH`
`refreshCurrentView` (`tui/app.go:562-607`) begins with `InvalidateAll()` — every
library, season, episode, and playlist cache — to update one checkmark. Its column
switch has no playlist cases, so `w` on a playlist item falls through to
`LoadLibrariesCmd` → `LibrariesLoadedMsg` → `ColumnStack.Reset` + full parallel
re-sync of everything. Even in the normal case the reload lands in `SetItems`, which
resets cursor/sort/filter (see B1). Marking a season watched episode-by-episode is
the app's worst interaction.
**Fix:** patch watch state in place in the store (item lists are already cached);
handle playlist column types; targeted invalidation only.

### A4. Stale `PlaylistsLoadedMsg` corrupts whatever column is on top   `HIGH`
`tui/app.go:454-460` applies `top.SetItems(msg.Playlists)` with no `validateContentID`
check (every other load handler has one), and the playlists column is never given a
ContentID (`navigation.go:191-209`). `SetItems` also rewrites `columnType` to
`ColumnTypePlaylists` (`list_column.go:344-346`). Slow playlist fetch + navigate
elsewhere → a "Movies" column containing playlists, where `x` now deletes playlists.
Also triggered async by `PlaylistDeletedMsg` (`app.go:519`).
**Fix:** synthetic ContentID for playlists + validate like the other five handlers.

### A5. Successful sync can report "sync cancelled"; sync counters drift   `HIGH`
The final done message in `SyncLibraryCmd` uses a non-blocking send
(`tui/commands.go:194-203`); if the 100-slot buffer is full (large library, many
parallel syncs), the done message is dropped, the channel closes, and
`readSyncProgress` fabricates `"sync cancelled"` (`commands.go:222-229`) — an error
state on a sync that succeeded. Separately, sync chains have no generation tag:
pressing `R` mid-sync lets old chains decrement the new `SyncingCount`
(`app.go:384-432`), which can wedge `m.Loading` permanently; `handleRefresh` also
double-increments an already-syncing library (`keyboard.go:231-238`).
**Fix:** blocking send for the terminal message (reader always drains to close);
tag sync messages with a generation and ignore stale ones.

### A6. Drill-down poisons the freshness timestamp with the local clock   `HIGH`
`FetchMovies/FetchShows/FetchMixedContent` save with `time.Now().Unix()` as the
"server timestamp" (`library/service.go:110,126,142`) while `SyncLibrary` saves the
server's `lib.UpdatedAt`. Both write `lib:{id}:ts`, and `IsValid` compares
`storedTS >= serverTS`. Local epoch (~1.75e9) exceeds Plex's `contentChangedAt`
counter and Jellyfin's `DateCreated` forever, so after any TUI drill-down the
timestamp check passes permanently. The v0.4.3 item-count check still catches
add/remove, but the timestamp leg is dead and count-neutral changes go undetected.
**Fix:** plumb the library's `UpdatedAt` into the `Fetch*` saves, or move to an
explicit fetched-at + TTL model consistently.

### A7. Seasons/episodes/playlists have no freshness model at all   `MEDIUM`
`SaveSeasons/SaveEpisodes/SavePlaylists/SavePlaylistItems` write no timestamp
(`store/store.go:301-318,393-405`); the TUI serves cached seasons/episodes
unconditionally (`navigation.go:291,317`). New episodes never appear on drill-down
without manual refresh; deletions linger forever.
**Fix:** fetched-at TTL per key, or validate child counts against the parent list's
`EpisodeCount`/`ChildCount` (already fetched).

### A8. Empty episode list mutates the column into a movies column   `MEDIUM`
`SetItems` sniffs `v[0].Type` to distinguish episodes from movies
(`list_column.go:331`); an empty `[]*MediaItem` falls into `WrapMovies` and rewrites
`columnType`, so `r`/`s` on an empty season take the movies code path. Related:
`SetItems(nil)` matches no case and silently doesn't clear; `columnType == 0` used as
"unset" but 0 is `ColumnTypeLibraries` (`list_column.go:350`).
**Fix:** typed `SetItems([]domain.ListItem)`; columnType immutable after construction.

### A9. Store read-promotion race resurrects invalidated data   `MEDIUM`
`store.get()` releases the read lock, reads Bolt unlocked, then re-locks to promote
into the memory map (`store/store.go:112-151`). An invalidation interleaved between
read and promote re-inserts deleted data into the memory cache, which is consulted
first — refreshed-away data served indefinitely. Dual-write is also non-atomic:
memory is updated before Bolt (`store.go:153-175`), data and ts go in two separate
transactions (`store.go:253-257`), and delete errors are ignored (`store.go:190,215`).
**Fix:** generation counter checked under the write lock before promotion (or drop
the memory layer — Bolt mmap reads are fast enough); write Bolt first; single tx for
data+ts.

### A10. Plex global search can't find TV shows   `MEDIUM`
Plex `Search` maps via `MapOnDeck`, which handles only `movie`/`episode`
(`plex/mapper.go:248-261`); Jellyfin includes `Series` (`jellyfin/mapper.go:312-321`).
Same query, different result classes per backend. Jellyfin caps at 50, Plex is
unbounded.
**Fix:** add the show case; align limits; rename `MapOnDeck`.

### A11. `KINO_*` env overrides are dead code   `MEDIUM`
`SetEnvPrefix`+`AutomaticEnv` without `SetEnvKeyReplacer` means nested keys map to
un-settable names (`KINO_SERVER.TOKEN`), and viper's `Unmarshal` doesn't see
AutomaticEnv-only keys without registered defaults (viper #761). No `SetDefault`
calls exist. `KINO_SERVER_TOKEN` etc. are silently ignored.
**Fix:** env key replacer + explicit `BindEnv` per key + a test.

### A12. cwd config + default-path saves = sign-out that doesn't sign out   `MEDIUM`
`AddConfigPath(".")` (`config.go:102`) can load `./config.yaml`, but `SaveConfig`/
`ClearServerConfig` always write to `~/.config/kino/`. Logout writes a cleared file
to the default path while the cwd file still holds the token and wins next launch —
and the token gets forked into a second file. Package-global viper also blocks
parallel tests and leaks `viper.Set` residue between Load/Save cycles.
**Fix:** per-instance `viper.New()` in a small ConfigStore that writes back to
`ConfigFileUsed()`; reconsider `AddConfigPath(".")`.

### A13. Sundry verified nits   `LOW`
- Plex `MarkPlayed/MarkUnplayed` omit `identifier=com.plexapp.plugins.library`
  (`plex/client.go:382-397`) — required on some PMS versions.
- `fetchAll` trusts `total`: `total=0` + non-empty page terminates after one page;
  offset pagination under concurrent mutation can dup/skip items — dedupe by ID
  before save (`library/service.go:258-296`).
- `PlayItemCmd` uses `context.Background()` with no timeout (`tui/commands.go:110`)
  while every sibling has one; a hung server wedges the command forever.
- Cache DB keyed by server URL only (`store/store.go:46-54`) — two accounts on one
  server share watch state and playlists. Include UserID in the hash.
- Divide-by-zero → "NaN%" in show/season inspector when `EpisodeCount == 0`
  (`inspector.go:391-393,427-429`).
- Byte-based `Truncate`/`Pad` (`styles.go:176-198`) mangle non-ASCII titles
  (mojibake, misalignment) — everyday input for a media library.
- `GetPlaylistMembership` is a serial N+1 network scan under the modal's single 30s
  timeout, and failures silently report "not a member" next to a remove toggle
  (`playlist/service.go:121-155`).
- Dangling nav-plan can resume after manual navigation and teleport the user
  (`navigation.go:406-408`; only Esc and ErrMsg clear it).

---

## B. Security / privacy

### B1. config.yaml (contains token) and log file written 0644   `HIGH`
viper defaults to 0644 (`WriteConfigAs` at `config.go:129,180,219`); the log file is
opened 0644 (`log/logger.go:33`). Any local user can read the Plex/Jellyfin token.
**Fix:** `viper.SetConfigPermissions(0o600)` + chmod existing on load; 0600 log file.

### B2. Auth token in `ps`-visible player args, mpv artifacts, and Debug logs   `HIGH`
Stream URLs embed the token (`plex/client.go:357`, `jellyfin/client.go:400-401`) and
are passed as argv to the player (`launcher.go:116-156`) — visible in
`/proc/*/cmdline` for the whole playback, persisted by mpv watch-later files, and
logged at Debug with args/url (`launcher.go:118,146,199`) into the 0644 log.
**Fix:** redact query tokens in every log line (small helper); prefer
`--http-header-fields` for mpv-family players; consider Plex transient tokens.

---

## C. UX findings

### C1. `SetItems` resets cursor, sort, and filter — five flows lose user state   `HIGH`
Root cause at `list_column.go:316-370` (cursor=0, `clearFilter()`, sort reset).
Hit by: mark watched/unwatched (via A3), refresh (issue #24), sort-apply clearing
filter (`ApplySort` → `clearFilter`, `list_column.go:1127`), re-drilling into a
level you just left (fresh column each time, `navigation.go:75`), and load
completion. One selection-preserving `ReplaceItems` (restore by ID, reapply
filter/sort) fixes the class.

### C2. Failed load = infinite spinner with no recovery hint   `HIGH`
`ErrMsg` never calls `SetLoading(false)` (nothing does except `SetItems`); the column
spins forever while the footer error clears after 5s (`app.go:359-371`,
`list_column.go:653-657`). Add an error state to the column: "✗ failed — r to retry".

### C3. Playlist delete is one unconfirmed keystroke; logout gets a modal   `HIGH`
`x` → `DeletePlaylistCmd` immediately (`keyboard.go:402-407`) — irreversible
server-side deletion. Reuse the confirm-state pattern.

### C4. Refresh blanks content (issue #24)   `HIGH`
Confirmed as filed; see issue. Libraries-level refresh already has the right pattern
(per-row spinner); content/seasons/episodes/playlists blank the column.

### C5. Keyboard semantics inconsistencies   `MEDIUM`
- Ctrl+C can't quit inside any modal/text input (modal routing precedes the quit
  check, `keyboard.go:36-43`); `q` quits in browse, applies-and-closes the playlist
  modal, does nothing in sort modal.
- `h` in the sort modal *applies ascending sort* instead of backing out
  (`components/keys.go:162-165`) — and via ApplySort destroys the active filter.
- `l` on a leaf item launches playback with resume (`keyboard.go:160-172`) though all
  docs describe it as "expand".
- Backspace after accepting a filter pops the column instead of editing the filter.

### C6. Silent no-op keys   `MEDIUM`
`w`/`u`/`p`/`Space` on Shows and Seasons silently do nothing (mark-season-watched is
a common expectation); `s` on libraries/seasons/playlists: nothing; `x` outside
playlists: nothing. Either implement or emit "Not available for shows".

### C7. Feedback gaps   `MEDIUM`
"Launched: X" status never clears (`app.go:341-343`, only status without a
ClearStatusCmd); no "Launching..." pending state between Enter and mpv appearing;
playlist modal opens async with no spinner (`keyboard.go:378-388`); modal header
never shows which item you're adding (computed then discarded,
`playlist_modal.go:209-215`).

### C8. Help screen and README diverge from the real keymap   `MEDIUM`
Help says "press any key to return" (only esc/?/q work); missing `x`, `n`,
backspace-as-back; README documents "`q` Quit/Back" (q never goes back) and calls
Ctrl+u/d "page" (they're half-page). Regenerate both from `DefaultKeyMap()`.

### C9. `R` teleports to root unconfirmed   `MEDIUM`
Covered in issue #24 discussion — separate PR agreed.

### C10. WSL: playback fallback is broken   `MEDIUM`  *(user-reported, confirmed)*
Auto-detect only probes Linux binary names, so a Windows-side `mpv.exe` (reachable
via WSL interop and usually on PATH) is invisible; the final fallback hardcodes
`xdg-open` (`launcher.go:196`), absent in typical WSL distros → raw exec error.
**Fix:** detect WSL (`WSL_DISTRO_NAME`/`/proc/sys/kernel/osrelease`); probe `.exe`
player variants after the Linux list; fall back `wslview` → `explorer.exe <url>` →
`powershell.exe Start-Process`; and replace the raw exec error with "no player found
— install mpv or set player.command".

---

## D. Architecture / design debt

### D1. No progress reporting/scrobbling; resume is powered by other clients   `HIGH`
`cmd.Start()` is never `Wait()`ed (zombie per playback) and no timeline/progress
endpoint exists anywhere. Watching in kino never marks watched or advances
`ViewOffset`; Resume only works if another client reported progress.
**Fix:** hold the child process, `Wait()` in a goroutine, report progress (mpv JSON
IPC for exact position; elapsed-wall-clock as fallback). Biggest single feature-level
gap in the app.

### D2. `domain.Store` is a 22-method god-interface; TUI reads it directly   `MEDIUM`
The TUI holds the whole store and hand-rolls the "cache else fetch" policy in 11
`getCached func() interface{}` closures (`navigation.go`), leaking key topology
(`currentLibID/currentShowID` plumbing) into the UI. Services always fetch; the TUI
decides staleness. Split into `LibraryCache`/`TVCache`/`PlaylistCache` and give
services cache-aware read APIs so policy lives in one layer.

### D3. ListColumn polymorphism: three mechanisms fighting   `MEDIUM`
`domain.ListItem` interface (its `GetItemType`/`GetDescription` never called),
columnType enum + unchecked type assertions (`list_column.go:718-739`), and
`SetItems(interface{})` sniffing. Five near-identical `renderXItem` methods
(`renderMixedItem` proves the interface suffices). Commit to `[]ListItem` + one
generic renderer with a per-type prefix hook.

### D4. Domain entities carry presentation   `MEDIUM`
`GetDescription()` returns UI copy ("3 Seasons"), `FormattedDuration`,
`FormattedFileSize`, zero-padded sort-key hacks (`entities.go`). Localization/
pluralization welded into the innermost layer; move to a TUI-side presenter.

### D5. Type identity expressed four ways   `MEDIUM`
`MediaType` iota, `Library.Type` raw strings with `default:`-means-mixed switches in
four places, `GetItemType()` string vocabulary, store wrapper tags (an episode in
mixed content serializes as `"movie"`, `store.go:416-433`). One typed enum + typed
LibraryType constants.

### D6. View() mutates state and duplicates layout   `MEDIUM`
`views.go:49-70` calls `SetSize`/`SetItem` during render, duplicating `updateLayout`
(`layout.go:66-109`); the inspector's Update-side path is masked dead code, and its
scroll machinery can never fire. Make View pure; single owner for layout and for the
inspector item.

### D7. Backend transport policy asymmetry   `MEDIUM`
Jellyfin: 60s timeout, 3×5xx retry (but no retry on network errors — backwards);
Plex: 30s, no retry at all. Neither policy covers hand-rolled mutations. One shared
transport helper with a single retry/timeout/mapping policy (same refactor as A2).

### D8. Error-handling texture   `LOW`
`domain.ErrServerOffline` returned bare, discarding the cause (DNS vs TLS vs refused
all render identically; several paths don't even log it); duplicate package-local
sentinels in jellyfin/plex auth shadow the domain ones with different messages;
services log-and-return so the same failure lands in the log 2-3×; 5xx bodies logged
unbounded up to 4× per request. Wrap with `%w`, delete local sentinels, log once at
the boundary, truncate bodies.

### D9. Setup & lifecycle friction   `LOW`
Setup saves config then exits ("Run kino again") instead of falling through to the
existing startup path (`main.go:183`); scheme-less URLs produce raw
`unsupported protocol scheme` errors; no `signal.NotifyContext` around the 5-minute
PIN wait. Mutable package-global keymaps defined twice (`tui/keys.go` vs
`components/keys.go`) that can drift.

### D10. Test coverage   `MEDIUM`
Two test files in the repo (both added this month). The most mechanically testable
code — 900 lines of pure mappers, `doRequest` error mapping, `detect.go` — has zero
coverage, and this review's bug list (A1, A2, A10, A13) is exactly the class those
tests catch. Priority: mapper golden tests from real API fixtures, `httptest`-based
client tests (401/5xx/network → sentinel), detect table tests.

---

## E. Dead code inventory (safe deletions)

- `InputModal` — `Show()` never called; routing, submit handler, overlay all
  unreachable. Either wire `n` → new-playlist input (finishing the feature) or delete.
- `domain.SearchClient` — required of every backend, implemented by both, called by
  no one. Search is cache-only and silently skips unsynced libraries with no
  indicator. Wire in as fallback or delete from `MediaSource`.
- `MediaSource.GetMediaItem` — no callers; dead weight on both backends.
- Episode search path — search never emits episodes; `navigateToSearchResult`'s
  episode branch is unreachable and pre-broken (empty nav targets).
- Unused: `ColumnStack.Depth/Parent`, `ListColumn.FindIndexByID/IsEmpty/Width/
  Height`, `Inspector.HasItem/Update`, `GlobalSearch.loading`, ~10 styles,
  `RenderProgressBar`, `views.go` `RenderSpinner` duplicate, empty if-block
  `app.go:408-416`, discarded `itemTitle` in playlist modal.

---

## F. Suggested sequencing

1. **PR: issue #24** (agreed) — non-blocking refresh + selection-preserving
   `ReplaceItems`. Fixes C1/C4 root cause.
2. **PR: mark-watched surgery** — in-place watch-state cache update; playlist cases
   in `refreshCurrentView`; kill `InvalidateAll`-on-toggle (A3). Biggest daily-use win.
3. **PR: correctness batch** — Jellyfin playlist-remove (A1), playlists ContentID
   (A4), sync done-message + generation (A5), empty-episode column mutation (A8).
4. **PR: security batch** — 0600 config/log, token redaction in logs (B1, B2).
5. **PR: shared transport per backend** — fixes A2, D7, D8, L-tier duplication; add
   client tests (D10) in the same PR.
6. **PR: WSL support** (C10) + launcher error message.
7. **PR: freshness model** — A6/A7 (plumb server ts + TTL for TV hierarchy).
8. **PR: `R` keep-position** (agreed follow-up to #24).
9. Ongoing: dead-code deletion (E), help/README regeneration (C8), then the larger
   D2/D3/D4 refactors as appetite allows — each unlocks simpler code but none is
   urgent.
