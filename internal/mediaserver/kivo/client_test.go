package kivo

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mmcdole/kino/internal/media"
)

const (
	movieLib = "01a12224-94d8-7144-81b1-87d8b41f390d"
	showLib  = "01a12269-7594-712d-ab86-3dfe2b2f068b"
	seriesID = "01a1227b-2659-73c4-bfe5-6a8ae96b477e"
	seasonID = "01a1227b-265a-7176-b148-5e600aa5ff7f"
	episode  = "01a1227b-265a-7177-b4fe-40aa18f5d0df"
	playFile = "01a14d87-75e8-70b0-9d7b-07bcce2ea634"
)

func testClient(t *testing.T, routes map[string]route) (*Client, *fakeKivo, string) {
	t.Helper()
	fake, srv := newFakeKivo(t, routes)
	c := NewClient(srv.URL, "tok", nil)
	c.api.RetryDelay = 0
	return c, fake, srv.URL
}

// Audiobooks libraries have no columns in kino, so they're left out.
func TestLibrariesLeaveOutAudiobooks(t *testing.T) {
	c, _, _ := testClient(t, map[string]route{"GET /libraries": {200, "libraries.json"}})
	libs, err := c.GetLibraries(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, l := range libs {
		names = append(names, l.Name+":"+l.Type)
	}
	want := "Movies:movie Movies Kids:movie TV:show TV Kids:show Anime:show"
	if got := strings.Join(names, " "); got != want {
		t.Fatalf("libraries = %s, want %s", got, want)
	}
}

// Kivo sends a browse's total on its first page only; the client keeps it
// for later pages so the catalog's progress stays right.
func TestBrowseKeepsFirstPageTotal(t *testing.T) {
	c, fake, _ := testClient(t, map[string]route{"GET /works": {200, "movies_page1.json"}})
	ctx := context.Background()
	movies, total, err := c.GetMovies(ctx, movieLib, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1999 || len(movies) != 2 {
		t.Fatalf("first page: %d movies of %d", len(movies), total)
	}
	m := movies[0]
	if m.Title != "The 'Burbs" || m.Year != 1989 || m.ViewOffset != 2744111*time.Millisecond || m.IsPlayed || m.LibraryID != movieLib || m.Type != media.TypeMovie {
		t.Fatalf("movie mapped as %+v", m)
	}
	if q := fake.requests()[0].query; !strings.Contains(q, "offset=0") || !strings.Contains(q, "libraryId="+movieLib) {
		t.Fatalf("browse query = %s", q)
	}

	fake.routes["GET /works"] = route{200, "movies_page2.json"}
	_, total, err = c.GetMovies(ctx, movieLib, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1999 {
		t.Fatalf("second page total = %d, want the first page's", total)
	}
}

// A series card has the unplayed count but not the episode count, which
// stays zero (unknown) rather than made up.
func TestShowsCarryUnplayedCountOnly(t *testing.T) {
	c, _, _ := testClient(t, map[string]route{"GET /works": {200, "shows_page1.json"}})
	shows, total, err := c.GetShows(context.Background(), showLib, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	s := shows[0]
	if total != 515 || s.Title != "The 'Burbs" || s.UnwatchedCount != 8 || s.EpisodeCount != 0 {
		t.Fatalf("show mapped as %+v (total %d)", s, total)
	}
}

func TestSeasonsAndEpisodes(t *testing.T) {
	c, fake, _ := testClient(t, map[string]route{"GET /works": {200, "seasons.json"}})
	ctx := context.Background()
	seasons, err := c.GetSeasons(ctx, seriesID)
	if err != nil {
		t.Fatal(err)
	}
	if len(seasons) != 1 || seasons[0].SeasonNum != 1 || seasons[0].EpisodeCount != 8 || seasons[0].ShowID != seriesID {
		t.Fatalf("seasons = %+v", seasons[0])
	}
	if q := fake.requests()[0].query; q != "parentId="+seriesID {
		t.Fatalf("children query = %s", q)
	}

	fake.routes["GET /works"] = route{200, "episodes.json"}
	eps, err := c.GetEpisodes(ctx, seasonID)
	if err != nil {
		t.Fatal(err)
	}
	e := eps[0]
	if e.Type != media.TypeEpisode || e.ShowID != seriesID || e.ParentID != seasonID || e.SeasonNum != 1 || e.EpisodeNum != 1 || e.ViewOffset != 730956*time.Millisecond {
		t.Fatalf("episode mapped as %+v", e)
	}
}

func TestMarkPlayedPatchesUserState(t *testing.T) {
	c, fake, _ := testClient(t, map[string]route{"PATCH /works/" + episode + "/user-state": {200, "user_state.json"}})
	if err := c.MarkPlayed(context.Background(), episode); err != nil {
		t.Fatal(err)
	}
	if err := c.MarkUnplayed(context.Background(), episode); err != nil {
		t.Fatal(err)
	}
	got := fake.requests()
	if string(got[0].body) != `{"played":true}` || string(got[1].body) != `{"played":false}` {
		t.Fatalf("bodies = %s, %s", got[0].body, got[1].body)
	}
}

// StartPlay sends the position and the player's capabilities, and resolves
// the play's media URLs, which are relative to /api/v1/.
func TestStartPlayFile(t *testing.T) {
	c, fake, base := testClient(t, map[string]route{"POST /plays": {201, "play_file.json"}})
	stream, err := c.StartPlay(context.Background(), episode, 730956*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	want := base + "/api/v1/plays/" + playFile + "/files/0"
	if stream.PlayID != playFile || len(stream.Parts) != 1 || stream.Parts[0].URL != want ||
		stream.Parts[0].Duration != 2502848*time.Millisecond || stream.Start != 730956*time.Millisecond {
		t.Fatalf("stream = %+v", stream)
	}
	var sent playCreate
	if err := json.Unmarshal(fake.requests()[0].body, &sent); err != nil {
		t.Fatal(err)
	}
	if sent.WorkID != episode || sent.PositionMs != 730956 || !sent.Capabilities.HLS || len(sent.Capabilities.Containers) == 0 {
		t.Fatalf("start request = %+v", sent)
	}
}

func TestStartPlayHLS(t *testing.T) {
	c, _, base := testClient(t, map[string]route{"POST /plays": {201, "play_hls.json"}})
	stream, err := c.StartPlay(context.Background(), episode, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(stream.Parts) != 1 || !strings.HasPrefix(stream.Parts[0].URL, base+"/api/v1/plays/") || !strings.HasSuffix(stream.Parts[0].URL, "/hls/index.m3u8") {
		t.Fatalf("hls stream = %+v", stream)
	}
}

// Refusals the user can act on read as sentences; the TUI shows them as
// they are.
func TestStartPlayRefusals(t *testing.T) {
	for _, tc := range []struct {
		status  int
		fixture string
		want    string
	}{
		{409, "problem_stream_limit.json", "Already playing on Living Room (Dune) and iPhone (Severance). Stop one to play here."},
		{503, "problem_transcode_busy.json", "The server's transcode disk is full."},
		{409, "problem_not_playable.json", "Can't play this here: its format can't be played or converted."},
	} {
		t.Run(tc.fixture, func(t *testing.T) {
			c, _, _ := testClient(t, map[string]route{"POST /plays": {tc.status, tc.fixture}})
			_, err := c.StartPlay(context.Background(), episode, 0)
			if !errors.Is(err, media.ErrPlaybackRefused) || err.Error() != tc.want {
				t.Fatalf("err = %v, want refusal %q", err, tc.want)
			}
		})
	}
}

func TestHeartbeatStopAndFallback(t *testing.T) {
	c, fake, _ := testClient(t, map[string]route{
		"PATCH /plays/" + playFile:              {204, ""},
		"POST /plays/" + playFile + "/stop":     {204, ""},
		"POST /plays/" + playFile + "/fallback": {201, "play_hls.json"},
	})
	ctx := context.Background()
	if err := c.ReportPlay(ctx, playFile, media.Progress{Position: 90 * time.Second, State: media.PlayPaused, SeekToFirstFrame: 350 * time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	next, err := c.FallbackPlay(ctx, playFile, 90*time.Second, "loading failed")
	if err != nil || next.PlayID == playFile || next.PlayID == "" {
		t.Fatalf("fallback = %+v, %v", next, err)
	}
	if err := c.StopPlay(ctx, playFile, 95*time.Second); err != nil {
		t.Fatal(err)
	}
	got := fake.requests()
	for i, want := range []string{
		`{"positionMs":90000,"state":"paused","seekToFirstFrameMs":350}`,
		`{"positionMs":90000,"code":"mediaSourceError","detail":"loading failed"}`,
		`{"positionMs":95000}`,
	} {
		if string(got[i].body) != want {
			t.Errorf("request %d body = %s, want %s", i, got[i].body, want)
		}
	}
}

// A heartbeat for a play that expired or an admin stopped is
// ErrItemNotFound, which the session reads as the play being gone.
func TestHeartbeatOnEndedPlay(t *testing.T) {
	c, _, _ := testClient(t, map[string]route{"PATCH /plays/" + playFile: {404, "problem_not_found.json"}})
	err := c.ReportPlay(context.Background(), playFile, media.Progress{State: media.PlayPlaying})
	if !errors.Is(err, media.ErrItemNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestNoFallbackLeft(t *testing.T) {
	c, _, _ := testClient(t, map[string]route{"POST /plays/" + playFile + "/fallback": {409, "problem_no_fallback.json"}})
	_, err := c.FallbackPlay(context.Background(), playFile, 0, "")
	if !errors.Is(err, media.ErrNoFallback) {
		t.Fatalf("err = %v", err)
	}
}

func TestUnauthorizedMapsToErrAuthFailed(t *testing.T) {
	c, _, _ := testClient(t, map[string]route{"GET /libraries": {401, "problem_unauthenticated.json"}})
	if _, err := c.GetLibraries(context.Background()); !errors.Is(err, media.ErrAuthFailed) {
		t.Fatalf("err = %v", err)
	}
}

func TestProbe(t *testing.T) {
	_, srv := newFakeKivo(t, map[string]route{"GET /server": {200, "server.json"}})
	if err := Probe(context.Background(), http.DefaultClient, srv.URL); err != nil {
		t.Fatalf("probe: %v", err)
	}
	_, other := newFakeKivo(t, map[string]route{})
	other.Config.Handler = http.NotFoundHandler()
	if err := Probe(context.Background(), http.DefaultClient, other.URL); err == nil {
		t.Fatal("a server without /api/v1/server probed as Kivo")
	}
}

// The device polls while the code waits (202), then gets a session token
// and looks up who signed in.
func TestDeviceCodeSignIn(t *testing.T) {
	polls := 0
	fake, srv := newFakeKivo(t, map[string]route{
		"POST /device-codes": {201, "device_code.json"},
		"GET /users/me":      {200, "users_me.json"},
	})
	fake.routes["POST /device-codes/poll"] = route{202, ""}
	inner := srv.Config.Handler
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/device-codes/poll") {
			polls++
			if polls == 2 {
				fake.mu.Lock()
				fake.routes["POST /device-codes/poll"] = route{201, "session.json"}
				fake.mu.Unlock()
			}
		}
		inner.ServeHTTP(w, r)
	})

	auth := NewAuthClient(srv.URL, nil)
	ctx := context.Background()
	dc, err := auth.StartDeviceCode(ctx, "kino on testhost", "kino-0123")
	if err != nil {
		t.Fatal(err)
	}
	if dc.Code != "F5WUFZ" || dc.secret != "device-secret" {
		t.Fatalf("device code = %+v", dc)
	}
	dc.interval, dc.expires = time.Millisecond, time.Now().Add(time.Minute)
	creds, err := auth.WaitForDeviceCode(ctx, dc)
	if err != nil {
		t.Fatal(err)
	}
	if creds.Token != "app-session-token" || creds.Username != "viewer" || creds.UserID == "" {
		t.Fatalf("credentials = %+v", creds)
	}
	got := fake.requests()
	if string(got[0].body) != `{"name":"kino on testhost","kind":"app","clientId":"kino-0123"}` {
		t.Fatalf("start body = %s", got[0].body)
	}
}

func TestDeviceCodeDeniedOrExpired(t *testing.T) {
	for status, want := range map[int]error{http.StatusForbidden: ErrDeviceCodeDenied, http.StatusGone: ErrDeviceCodeExpired} {
		_, srv := newFakeKivo(t, nil)
		srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) })
		auth := NewAuthClient(srv.URL, nil)
		_, err := auth.WaitForDeviceCode(context.Background(), DeviceCode{secret: "s", interval: time.Millisecond})
		if !errors.Is(err, want) {
			t.Errorf("%d: err = %v, want %v", status, err, want)
		}
	}
}
