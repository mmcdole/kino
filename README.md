# Kino

![Kino Demo](demo.gif?v=2)

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
| `Enter` | Play/Select item |
| `Space` | Manage playlists |
| `f` | Global search |
| `/` | Local filter (current column) |
| `s` | Sort options |
| `i` | Toggle inspector panel |
| `r` | Refresh current library |
| `R` | Refresh all libraries |
| `g` | Jump to top |
| `G` | Jump to bottom |
| `Ctrl+u` / `Ctrl+d` | Page up/down |
| `?` | Show help |
| `q` | Quit/Back |

## Configuration

Config file: `~/.config/kino/config.yaml` (created on first run).

Kino auto-detects video players (mpv, VLC, IINA, Celluloid, etc.) with resume support. See `config.example.yaml` for custom player setup and all options.

## License

MIT
