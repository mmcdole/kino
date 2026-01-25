# Kino

![Kino Demo](demo.gif)

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

**Download** from [Releases](https://github.com/mmcdole/kino/releases) or install with Go:

```bash
go install github.com/mmcdole/kino/cmd/kino@latest
```

### First Run

Launch Kino and follow the interactive setup:

```bash
kino
```

You'll be prompted to enter your server URL and authenticate.

## Usage

### Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `↑` `↓` `j` `k` | Navigate up/down |
| `←` `→` `h` `l` | Navigate left/right (columns) |
| `Enter` | Play/Select item |
| `f` | Global search (when libraries synced) |
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

Kino stores configuration in `~/.config/kino/config.yaml` (created on first run).

### Video Player Setup

**Default Behavior:**

Kino auto-detects installed video players in priority order:

| macOS | Linux |
|-------|-------|
| IINA, mpv, VLC | mpv, VLC, Celluloid, Haruna, smplayer, mplayer |

Resume playback works automatically with detected players.

**Custom Player (Optional):**

```yaml
player:
  command: "mpv"
  args:
    - "--no-terminal"
  start_flag: "--start=%d"  # Only needed for unknown players
```

<details>
<summary><b>Known Players & Resume Flags</b></summary>

| Player | Platforms | Resume Flag |
|--------|-----------|-------------|
| mpv | macOS, Linux | `--start=%d` |
| VLC | macOS, Linux | `--start-time=%d` |
| IINA | macOS | `--mpv-start=%d` |
| Celluloid | Linux | `--mpv-start=%d` |
| Haruna | Linux | `--start=%d` |
| smplayer | Linux | `-ss %d` |
| mplayer | Linux | `-ss %d` |

For unlisted players, set `start_flag` with `%d` as the seconds placeholder.
</details>

See `config.example.yaml` for all options.

## Contributing

Contributions welcome! Please open an issue or PR.

## License

MIT
