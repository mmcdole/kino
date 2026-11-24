package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/mmcdole/kino/internal/catalog"
	"github.com/mmcdole/kino/internal/media"
	"github.com/mmcdole/kino/internal/menu"
)

// Launcher mode: kino lists the library for a dmenu-style launcher and plays,
// or prints the stream of, the line it returns.
//
//	kino menu | fuzzel --dmenu | kino play
//	kino pick            # the same, with a detected or configured launcher
//	kino pick --url      # print the stream URL instead of playing
const menuUsage = `Usage:
  kino menu [--refresh]          List movies and shows, one per line
  kino play [line]               Play the line (or stdin): a movie resumes,
                                 a show plays its next unwatched episode
  kino url [line]                Print the line's stream URL(s) instead
  kino pick [--url] [--query text] [--refresh] [-- launcher argv...]
                                 Show the list in a launcher and play the pick;
                                 a show opens its episodes

The launcher is menu.command in the config, or the first found of:
vicinae dmenu, fuzzel --dmenu, rofi -dmenu, wofi --dmenu, dmenu.
`

// menuCommands are the launcher-mode subcommands main dispatches here.
var menuCommands = map[string]bool{"menu": true, "play": true, "url": true, "pick": true}

func runMenuCommand(name string, args []string) error {
	fs := flag.NewFlagSet("kino "+name, flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, menuUsage) }
	refresh := fs.Bool("refresh", false, "fetch the library from the server instead of the cache")
	asURL, query := false, ""
	if name == "pick" {
		fs.BoolVar(&asURL, "url", false, "print the stream URL instead of playing")
		fs.StringVar(&query, "query", "", "open the launcher with this search typed in")
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	a, err := openApp()
	if err != nil {
		return err
	}
	defer a.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch name {
	case "menu":
		entries, err := listEntries(ctx, a, *refresh)
		if err != nil {
			return err
		}
		return writeLabels(os.Stdout, entries)
	case "play", "url":
		line, err := lineArg(fs.Args())
		if err != nil {
			return err
		}
		return choose(ctx, a, line, name == "url", *refresh)
	default: // pick
		entries, err := listEntries(ctx, a, *refresh)
		if err != nil {
			return err
		}
		argv := fs.Args()
		if len(argv) == 0 {
			argv = a.cfg.Menu.Command
		}
		if len(argv) == 0 {
			if argv = detectLauncher(); argv == nil {
				return errors.New("no launcher found: install vicinae, fuzzel, rofi, wofi or dmenu, or set menu.command")
			}
		}
		line, err := runLauncher(ctx, withQuery(argv, query), entries)
		if err != nil || line == "" {
			return err // an empty pick is a dismissed launcher
		}
		err = chooseFrom(ctx, a, entries, line, asURL, argv)
		if errors.Is(err, errDismissed) {
			return nil
		}
		return err
	}
}

// lineArg is the chosen line from the arguments, or else from stdin.
func lineArg(args []string) (string, error) {
	if len(args) > 0 {
		return strings.Join(args, " "), nil
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", err
	}
	line := strings.TrimSpace(string(data))
	if line == "" {
		return "", errors.New("nothing chosen: pass a line from 'kino menu' as an argument or on stdin")
	}
	return line, nil
}

func writeLabels(w io.Writer, entries []menu.Entry) error {
	var buf bytes.Buffer
	for _, e := range entries {
		buf.WriteString(e.Label)
		buf.WriteByte('\n')
	}
	_, err := w.Write(buf.Bytes())
	return err
}

// listEntries builds the menu from the cache, so a launcher opens at once.
// Only what was never cached, or everything with refresh, comes from the
// server; kino's own browsing keeps the cache current.
func listEntries(ctx context.Context, a *app, refresh bool) ([]menu.Entry, error) {
	libs, err := a.load(ctx, catalog.Resource{Kind: catalog.Libraries}, refresh)
	if err != nil {
		return nil, fmt.Errorf("loading libraries: %w", err)
	}
	var libraries []menu.Library
	for _, item := range libs {
		lib, ok := item.(*media.Library)
		if !ok {
			continue
		}
		items, err := a.load(ctx, catalog.LibraryResource(*lib), refresh)
		if err != nil {
			return nil, fmt.Errorf("loading %s: %w", lib.Name, err)
		}
		libraries = append(libraries, menu.Library{Library: *lib, Items: items})
	}
	return menu.Entries(libraries), nil
}

// load returns r's cached items when there are any, else asks the server.
func (a *app) load(ctx context.Context, r catalog.Resource, refresh bool) ([]media.ListItem, error) {
	if !refresh {
		if entry, ok := a.cache.Load(r.Key()); ok {
			return entry.Items, nil
		}
	}
	policy := catalog.Revalidate
	if refresh {
		policy = catalog.Refresh
	}
	snapshot, err := a.catalog.Load(ctx, r, policy)
	if err != nil && len(snapshot.Items) == 0 {
		return nil, err
	}
	return snapshot.Items, nil
}

func choose(ctx context.Context, a *app, line string, asURL, refresh bool) error {
	entries, err := listEntries(ctx, a, refresh)
	if err != nil {
		return err
	}
	return chooseFrom(ctx, a, entries, line, asURL, nil)
}

// errDismissed is a launcher closed without a pick partway through.
var errDismissed = errors.New("dismissed")

// chooseFrom plays, or prints the stream of, the entry line names. With a
// launcher, a show opens its episodes in it; without one it plays the next.
func chooseFrom(ctx context.Context, a *app, entries []menu.Entry, line string, asURL bool, launcher []string) error {
	entry, ok := menu.Find(entries, line)
	if !ok {
		return fmt.Errorf("%q isn't in the library list; pass a line exactly as 'kino menu' prints it", strings.TrimSpace(line))
	}
	item, err := playable(ctx, a, entry, launcher)
	if err != nil {
		return err
	}
	resume := item.ShouldResume()
	if asURL {
		return printStream(ctx, a, item, resume)
	}
	if resume {
		return a.playback.Resume(ctx, *item)
	}
	return a.playback.Play(ctx, *item)
}

// playable is the item to play for an entry, with current watch state: the
// movie itself, or a show's episode picked in the launcher, or its next.
func playable(ctx context.Context, a *app, entry menu.Entry, launcher []string) (*media.Item, error) {
	switch v := entry.Item.(type) {
	case *media.Item:
		// The menu came from the cache; the resume point must not. Browse
		// trusts a library fetched in the last few minutes and otherwise
		// fetches it again.
		r := catalog.LibraryResource(entry.Library)
		snapshot, err := a.catalog.Load(ctx, r, catalog.Browse)
		if err == nil {
			for _, item := range snapshot.Items {
				if fresh, ok := item.(*media.Item); ok && fresh.ID == v.ID {
					return fresh, nil
				}
			}
		}
		return v, nil
	case *media.Show:
		seasons, err := loadSeasons(ctx, a, entry.Library, v)
		if err != nil {
			return nil, err
		}
		if launcher == nil {
			ep, ok := menu.NextEpisode(seasons)
			if !ok {
				return nil, fmt.Errorf("%s has no episodes", v.Title)
			}
			return ep, nil
		}
		episodes := menu.Episodes(seasons)
		if len(episodes) == 0 {
			return nil, fmt.Errorf("%s has no episodes", v.Title)
		}
		line, err := runLauncher(ctx, withPrompt(launcher, v.Title), episodes)
		if err != nil {
			return nil, err
		}
		if line == "" {
			return nil, errDismissed
		}
		picked, ok := menu.Find(episodes, line)
		if !ok {
			return nil, fmt.Errorf("%q isn't an episode of %s", line, v.Title)
		}
		return picked.Item.(*media.Item), nil
	default:
		return nil, fmt.Errorf("can't play %s", entry.Label)
	}
}

// loadSeasons fetches a show's seasons and episodes with current watch state.
func loadSeasons(ctx context.Context, a *app, lib media.Library, show *media.Show) ([]menu.Season, error) {
	r := catalog.Resource{Kind: catalog.Seasons, ID: show.ID, LibraryID: lib.ID, ShowID: show.ID}
	snapshot, err := a.catalog.Load(ctx, r, catalog.Revalidate)
	if err != nil {
		return nil, fmt.Errorf("loading %s's seasons: %w", show.Title, err)
	}
	var seasons []menu.Season
	for _, item := range snapshot.Items {
		season, ok := item.(*media.Season)
		if !ok {
			continue
		}
		r := catalog.Resource{Kind: catalog.Episodes, ID: season.ID, LibraryID: lib.ID, ShowID: show.ID}
		eps, err := a.catalog.Load(ctx, r, catalog.Revalidate)
		if err != nil {
			return nil, fmt.Errorf("loading %s %s: %w", show.Title, season.Title, err)
		}
		s := menu.Season{Season: season}
		for _, item := range eps.Items {
			if ep, ok := item.(*media.Item); ok {
				s.Episodes = append(s.Episodes, ep)
			}
		}
		seasons = append(seasons, s)
	}
	return seasons, nil
}

// printStream starts a play and prints its URLs, one per part. Nothing
// reports progress for it: a Kivo play stays open while the player keeps
// fetching, and expires two minutes after it stops.
func printStream(ctx context.Context, a *app, item *media.Item, resume bool) error {
	var at time.Duration
	if resume {
		at = item.ViewOffset
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	stream, err := a.client.StartPlay(ctx, item.ID, at, true)
	if err != nil {
		return err
	}
	if len(stream.Parts) == 0 {
		return media.ErrItemNotFound
	}
	for _, part := range stream.Parts {
		fmt.Println(part.URL)
	}
	return nil
}

// launchers are tried in order when menu.command isn't set.
var launchers = [][]string{
	{"vicinae", "dmenu", "--navigation-title", "Kino", "--placeholder", "Search", "--no-quick-look"},
	{"fuzzel", "--dmenu", "--prompt", "kino> "},
	{"rofi", "-dmenu", "-i", "-p", "kino"},
	{"wofi", "--dmenu", "-i", "-p", "kino"},
	{"dmenu", "-i", "-p", "kino"},
}

// withPrompt titles a second launcher list: it replaces the value of the
// launcher's title or prompt flag, when argv has one.
func withPrompt(argv []string, title string) []string {
	out := slices.Clone(argv)
	for i := 0; i+1 < len(out); i++ {
		switch out[i] {
		case "--navigation-title", "-n":
			out[i+1] = title
		case "--prompt", "-p":
			out[i+1] = title + "> "
		}
	}
	return out
}

// queryFlags are the flags known launchers take to start with a search
// typed in; a launcher without one opens with an empty search.
var queryFlags = map[string]string{
	"vicinae": "--query",
	"fuzzel":  "--search",
	"rofi":    "-filter",
	"wofi":    "--search",
}

// withQuery adds query to argv as the launcher's initial search.
func withQuery(argv []string, query string) []string {
	if query == "" || len(argv) == 0 {
		return argv
	}
	flag, ok := queryFlags[filepath.Base(argv[0])]
	if !ok {
		return argv
	}
	return append(slices.Clone(argv), flag, query)
}

func detectLauncher() []string {
	for _, argv := range launchers {
		if _, err := exec.LookPath(argv[0]); err == nil {
			return argv
		}
	}
	return nil
}

// runLauncher feeds the labels to the launcher and returns the line picked,
// or "" when it was dismissed (launchers exit non-zero with no output).
func runLauncher(ctx context.Context, argv []string, entries []menu.Entry) (string, error) {
	var in, out, errOut bytes.Buffer
	if err := writeLabels(&in, entries); err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = &in, &out, &errOut
	err := cmd.Run()
	line := strings.TrimSpace(out.String())
	if line != "" {
		return strings.SplitN(line, "\n", 2)[0], nil
	}
	var exit *exec.ExitError
	if err == nil || errors.As(err, &exit) && strings.TrimSpace(errOut.String()) == "" {
		return "", nil
	}
	if msg := strings.TrimSpace(errOut.String()); msg != "" {
		return "", fmt.Errorf("%s: %s", argv[0], msg)
	}
	return "", fmt.Errorf("running %s: %w", argv[0], err)
}

// reportError shows a launcher-mode error where its user will see it: a
// launcher's commands have no terminal, so off one it is a notification too.
func reportError(err error) {
	fmt.Fprintf(os.Stderr, "kino: %v\n", err)
	if info, statErr := os.Stderr.Stat(); statErr == nil && info.Mode()&os.ModeCharDevice != 0 {
		return
	}
	if path, lookErr := exec.LookPath("notify-send"); lookErr == nil {
		_ = exec.Command(path, "--app-name=kino", "kino", err.Error()).Run()
	}
}
