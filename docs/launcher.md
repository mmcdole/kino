# Launcher Mode

`kino pick` shows your library in a dmenu-style launcher and plays the title
you pick. Any launcher that reads lines on stdin and prints the selected line
works.

```bash
kino pick
kino pick --query dune    # start with "dune" in the search box
kino pick --url           # print the stream URL instead of playing
```

Movies resume from the saved position. Shows open a second list of episodes.
The first line is the next unwatched episode. Watched episodes are marked ✓
and partly watched ones ◐:

```
▶ Up next: S02E04  Woe's Hollow
S01E01  Good News About Hell  ✓
S02E03  Who Is Alive?  ◐
S02E04  Woe's Hollow
```

Kino reports playback progress to the server, as it does from the TUI.

## Choosing a launcher

Kino uses the first one installed: vicinae, fuzzel, rofi, wofi, dmenu. To set
one in the config:

```yaml
menu:
  command: ["fuzzel", "--dmenu"]
```

A command after `--` overrides the config:

```bash
kino pick -- rofi -dmenu -i -p movies
```

`--query` uses the launcher's search option (`--query`, `--search` or
`-filter`). dmenu has none and ignores it.

## Key binding

KDE Plasma: System Settings → Keyboard → Shortcuts → Add New → Command, then
enter `kino pick` and set a key.

Hyprland:

```
bind = SUPER, M, exec, kino pick
```

## Vicinae

To add Kino to Vicinae's root search, save this as
`~/.local/share/vicinae/scripts/kino-search.sh` and run Reload Script
Directories in Vicinae:

```bash
#!/bin/bash
# @vicinae.schemaVersion 1
# @vicinae.title Search Media
# @vicinae.mode silent
# @vicinae.icon 🎬
# @vicinae.keywords ["kino", "movie", "show", "watch"]
# @vicinae.argument1 { "type": "text", "placeholder": "title", "optional": true }

exec kino pick --query "${1:-}"
```

## Scripting

| Command | Description |
|---|---|
| `kino menu` | Print every movie and show, one per line |
| `kino play <line>` | Play a line from `kino menu` (or read it from stdin). A show plays its next episode |
| `kino url <line>` | Print the stream URL for a line |

```bash
kino menu | fuzzel --dmenu | kino play
mpv "$(kino url 'Heat (1995)')"
```

`kino menu` reads the local cache. Use `--refresh` to fetch from the server.
If the TUI is open, the cache is locked and `kino menu` fetches from the server.

URLs printed by `kino url` don't report progress. On Kivo the URL expires two
minutes after the player stops requesting it.

Errors are written to stderr and also sent as a desktop notification, since a
launcher doesn't show stderr.
