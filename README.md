# Kino

![Kino Demo](demo.gif?v=3)

> A fast terminal client for browsing and playing media from Plex and Jellyfin servers

## Features

-  Fuzzy search across your entire library
-  Keyboard-first interface with Vim-style navigation
-  Playlist management
-  Watch status tracking and smart resume
-  Inspector panel for detailed metadata
-  Fast, cached browsing with progressive loading

## Quick Start

### Installation

**Download** from [Releases](https://github.com/mmcdole/kino/releases) or install with Go:

```bash
go install github.com/mmcdole/kino/cmd/kino@latest
```

### First Run

Launch Kino and follow the interactive setup:

```bash
kino
```

You'll be prompted to enter your server URL. Kino automatically detects whether it's a Plex or Jellyfin server and guides you through the appropriate authentication.

## Usage

### Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `↑` `↓` `j` `k` | Navigate up/down |
| `←` `→` `h` `l` | Navigate left/right (columns) |
| `Backspace` | Back |
| `Enter` | Play / drill in |
| `p` | Play from start |
| `w` / `u` | Mark watched / unwatched |
| `Space` | Manage playlists |
| `x` | Delete playlist / remove item (in playlists) |
| `f` | Global search |
| `/` | Local filter (current column) |
| `s` | Sort options |
| `i` | Toggle inspector panel |
| `r` | Refresh current view |
| `R` | Refresh all libraries |
| `g` / `G` | Jump to top / bottom |
| `PgUp` / `PgDn` | Page up/down |
| `Ctrl+u` / `Ctrl+d` | Half page up/down |
| `?` | Show help |
| `L` | Logout |
| `q` / `Ctrl+c` | Quit |

## Configuration

Config file: `~/.config/kino/config.yaml` (created on first run).

Kino auto-detects video players (mpv, VLC, IINA, Celluloid, etc.) with resume support. See `config.example.yaml` for custom player setup and all options.

On WSL, Kino detects Windows-side players (PotPlayer, mpv.exe, VLC) from both `PATH` and Windows App Paths, so normal GUI installations work without extra configuration. Native Windows builds use the same detection.

If no media player is available, Kino opens the raw media URL with the platform's default URL handler. This is a best-effort browser fallback: resume is unavailable and some MKV/audio-codec combinations may play without audio. Installing mpv, VLC, or PotPlayer is recommended for reliable playback.

## License

MIT
