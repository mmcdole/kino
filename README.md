# Kino

[VIDEO PLACEHOLDER - USER WILL ADD]

> A fast, keyboard-driven TUI for browsing and playing media from Plex and Jellyfin servers

## Features

-  Browse movies and TV shows from Plex or Jellyfin
-  Fuzzy search across your entire library
-  Keyboard-first interface with Vim-style navigation
-  Miller column layout for intuitive browsing
-  Watch status tracking and smart resume
-  Inspector panel for detailed metadata
-  Fast, cached browsing with progressive loading

## Quick Start

### Installation

**From source:**
```bash
git clone https://github.com/USER/kino.git
cd kino
go build -o kino cmd/kino/main.go
```

**Or install directly:**
```bash
go install github.com/USER/kino/cmd/kino@latest
```

### First Run

Launch Kino and follow the interactive setup:

```bash
kino
```

You'll be prompted to:
1. Enter your Plex or Jellyfin server URL
2. Authenticate (opens browser for Plex, prompts for credentials on Jellyfin)
3. Select default libraries to browse

That's it! Start browsing your media.

## Usage

### Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `↑` `↓` `j` `k` | Navigate up/down |
| `←` `→` `h` `l` | Navigate left/right (columns) |
| `Enter` | Play/Select item |
| `/` | Global search (when libraries synced) |
| `f` | Local filter (current column) |
| `s` | Sort options |
| `i` | Toggle inspector panel |
| `r` | Refresh current library |
| `R` | Refresh all libraries |
| `gg` | Jump to top |
| `G` | Jump to bottom |
| `Ctrl+u` / `Ctrl+d` | Page up/down |
| `?` | Show help |
| `q` | Quit/Back |

## Configuration

Kino stores configuration in `~/.config/kino/config.yaml` (created on first run).

### Video Player Setup

**Default Behavior:**
- **macOS**: Auto-detects IINA, VLC, or mpv
- **Linux/Windows**: Uses system default player

**Enable Resume (Optional):**

To start playback from where you left off, configure a specific player:

```yaml
player:
  command: "mpv"  # or "vlc", "iina", "celluloid", "haruna"
  args:
    - "--no-terminal"
```

<details>
<summary><b>Supported Players & Custom Configuration</b></summary>

| Player | Platforms | Resume Flag |
|--------|-----------|-------------|
| mpv | All | `--start=` |
| VLC | All | `--start-time=` |
| IINA | macOS | `--mpv-start=` |
| Celluloid | Linux | `--mpv-start=` |
| Haruna | Linux (KDE) | `--mpv-start=` |
| PotPlayer | Windows | `/seek=` |

For custom players:
```yaml
player:
  command: "/path/to/player"
  start_flag: "--seek="
```
</details>

### Other Settings

```yaml
ui:
  theme: "default"          # or "dark", "light"
  grid_columns: 4           # Grid view column count
  default_view: "grid"      # or "list"

preferences:
  show_watch_status: true   # Show ●/◐/✓ indicators

logging:
  level: "INFO"             # DEBUG, INFO, WARN, ERROR
  file: "~/.local/share/kino/kino.log"
```

See `config.example.yaml` for all options.

## Features Deep Dive

### Miller Column Navigation
Kino uses a multi-column interface showing your navigation context:
`Libraries → Movies → Movie Details` or `TV Shows → Show → Season → Episode`

### Watch Status
- ● Orange = Unwatched
- ◐ Orange + % = In Progress
- ✓ Green = Watched

### Inspector Panel
Press `i` to toggle detailed metadata including summary, runtime, release date, and watch progress.

### Search
- **Global Search** (`/`): Search across all synced libraries
- **Local Filter** (`f`): Filter current column in real-time
- Both use fuzzy matching for forgiving searches

## Server Compatibility

- ✅ Plex Media Server
- ✅ Jellyfin

Server type is auto-detected during setup.

## Contributing

Contributions welcome! Please open an issue or PR.

## License

MIT
