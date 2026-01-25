package adapter

import (
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Launcher launches media URLs in an external player
type Launcher struct {
	command   string   // configured player command, empty for system default
	args      []string // additional arguments for the player
	startFlag string   // offset flag prefix, e.g., "--start=" or "-ss "
	logger    *slog.Logger
}

// launchPath defines a single way to launch a player
type launchPath struct {
	path         string   // Command path: "/usr/bin/mpv", "vlc", or "open-a:AppName"
	openFlags    []string // For "open-a:" paths only - flags for macOS open command (e.g., ["-n"])
	argSeparator string   // Separator before player args (e.g., "--" for iina-cli)
}

// playerConfig defines platform-specific launch configurations for a player
type playerConfig struct {
	offsetFlag string                  // Resume offset flag (e.g., "--start=")
	platforms  map[string][]launchPath // Platform -> launch paths to try in order
}

// players registry - single source of truth for all player configuration
var players = map[string]playerConfig{
	"mpv": {
		offsetFlag: "--start=",
		platforms: map[string][]launchPath{
			"darwin":  {{path: "mpv"}},
			"linux":   {{path: "mpv"}},
			"windows": {{path: "mpv"}},
		},
	},
	"vlc": {
		offsetFlag: "--start-time=",
		platforms: map[string][]launchPath{
			"darwin": {
				{path: "vlc"},
				{path: "open-a:VLC"}, // No openFlags - VLC doesn't need -n
			},
			"linux":   {{path: "vlc"}},
			"windows": {{path: "vlc"}},
		},
	},
	"iina": {
		offsetFlag: "--mpv-start=",
		platforms: map[string][]launchPath{
			"darwin": {
				{path: "open-a:IINA", openFlags: []string{"-n"}}, // IINA needs -n for new windows
			},
		},
	},
	"celluloid": {
		offsetFlag: "--mpv-start=",
		platforms: map[string][]launchPath{
			"linux": {{path: "celluloid"}},
		},
	},
	"haruna": {
		offsetFlag: "--mpv-start=",
		platforms: map[string][]launchPath{
			"linux": {{path: "haruna"}},
		},
	},
	"potplayer": {
		offsetFlag: "/seek=",
		platforms: map[string][]launchPath{
			"windows": {{path: "PotPlayerMini64.exe"}, {path: "PotPlayerMini.exe"}},
		},
	},
}

// candidatePlayers defines the preferred player order for each platform
var candidatePlayers = map[string][]string{
	"darwin":  {"iina", "vlc", "mpv"},
	"linux":   {"mpv", "celluloid", "haruna", "vlc"},
	"windows": {"vlc", "mpv", "potplayer"},
}

// NewLauncher creates a new Launcher with optional auto-detection of offset flags
func NewLauncher(command string, args []string, startFlag string, logger *slog.Logger) *Launcher {
	if logger == nil {
		logger = slog.Default()
	}

	// Auto-detect start flag for known players if not explicitly configured
	resolvedFlag := startFlag
	if resolvedFlag == "" && command != "" {
		base := filepath.Base(command)
		// Strip any extension (for Windows .exe)
		base = strings.TrimSuffix(base, filepath.Ext(base))
		base = strings.ToLower(base)

		// Look up from players registry
		if playerCfg, ok := players[base]; ok && playerCfg.offsetFlag != "" {
			resolvedFlag = playerCfg.offsetFlag
			logger.Debug("auto-detected player offset flag", "player", base, "flag", resolvedFlag)
		}
	}

	return &Launcher{
		command:   command,
		args:      args,
		startFlag: resolvedFlag,
		logger:    logger,
	}
}

// tryOpenWithApp attempts to open URL with a specific macOS app using "open -a"
// Returns nil if the app exists and launched successfully, error otherwise
func tryOpenWithApp(appName string, url string, playerArgs []string, openFlags []string) error {
	// Copy openFlags to avoid modifying the original slice
	cmdArgs := make([]string, len(openFlags))
	copy(cmdArgs, openFlags)

	cmdArgs = append(cmdArgs, "-a", appName)
	if len(playerArgs) > 0 {
		cmdArgs = append(cmdArgs, "--args")
		cmdArgs = append(cmdArgs, playerArgs...)
	}
	cmdArgs = append(cmdArgs, url)

	cmd := exec.Command("open", cmdArgs...)
	// Run() waits for command to complete and returns error if app not found
	return cmd.Run()
}

// tryLaunchWithCommand attempts to launch URL with a CLI command (Linux/Windows)
// Returns nil if command exists in PATH and launched, error otherwise
func tryLaunchWithCommand(command string, url string, args []string) error {
	// Check if command exists in PATH
	if _, err := exec.LookPath(command); err != nil {
		return err
	}

	cmdArgs := append(args, url)
	cmd := exec.Command(command, cmdArgs...)
	return cmd.Start() // Start async, don't wait
}

// detectAndLaunch tries candidate players in order using configured launch paths
// Returns the player name that succeeded, or empty string if all failed
func detectAndLaunch(url string, startOffset time.Duration, logger *slog.Logger) (string, error) {
	candidates, ok := candidatePlayers[runtime.GOOS]
	if !ok {
		candidates = candidatePlayers["linux"] // default
	}

	for _, playerName := range candidates {
		player, exists := players[playerName]
		if !exists {
			continue
		}

		// Get platform-specific launch paths
		launchPaths, ok := player.platforms[runtime.GOOS]
		if !ok {
			logger.Debug("player not available on this platform", "player", playerName, "platform", runtime.GOOS)
			continue
		}

		// Build offset args once per player
		offsetArgs := []string{}
		if startOffset > 0 && player.offsetFlag != "" {
			if strings.HasSuffix(player.offsetFlag, " ") {
				offsetArgs = append(offsetArgs, strings.TrimSuffix(player.offsetFlag, " "))
				offsetArgs = append(offsetArgs, fmt.Sprintf("%.0f", startOffset.Seconds()))
			} else {
				offsetArgs = append(offsetArgs, fmt.Sprintf("%s%.0f", player.offsetFlag, startOffset.Seconds()))
			}
		}

		// Try each launch path
		for _, lp := range launchPaths {
			var err error

			if strings.HasPrefix(lp.path, "open-a:") {
				appName := strings.TrimPrefix(lp.path, "open-a:")
				err = tryOpenWithApp(appName, url, offsetArgs, lp.openFlags)
			} else {
				// Direct command execution
				cmdArgs := offsetArgs
				if lp.argSeparator != "" && len(offsetArgs) > 0 {
					// Prepend separator before offset args (e.g., iina-cli -- --mpv-start=120)
					cmdArgs = append([]string{lp.argSeparator}, offsetArgs...)
				}
				err = tryLaunchWithCommand(lp.path, url, cmdArgs)
			}

			if err == nil {
				logger.Info("launched with detected player", "player", playerName, "path", lp.path)
				return playerName, nil
			}

			logger.Debug("launch path not available", "player", playerName, "path", lp.path, "error", err)
		}
	}

	return "", fmt.Errorf("no candidate players found")
}

// Launch opens a media URL in the configured player or system default
func (l *Launcher) Launch(url string, startOffset time.Duration) error {
	// Tier 1: User configured a specific player
	if l.command != "" {
		l.logger.Info("using configured player", "command", l.command)
		return l.launchConfigured(url, startOffset)
	}

	// Tier 2: Try candidate chain (IINA → VLC → mpv on macOS, etc.)
	if _, err := detectAndLaunch(url, startOffset, l.logger); err == nil {
		return nil // Successfully launched with detected player
	}

	// Tier 3: Fall back to system default (open/xdg-open/start)
	l.logger.Info("no candidate players found, using system default")
	return l.launchDefault(url)
}

// launchConfigured launches the media using the configured player
func (l *Launcher) launchConfigured(url string, startOffset time.Duration) error {
	args := append([]string{}, l.args...)

	// Inject start offset if we have a flag configured/detected
	if startOffset > 0 && l.startFlag != "" {
		// Handle flags that need a space (like "-ss 120") vs no space ("--start=120")
		if strings.HasSuffix(l.startFlag, " ") {
			// Flag like "-ss " needs value as separate arg
			args = append(args, strings.TrimSuffix(l.startFlag, " "))
			args = append(args, fmt.Sprintf("%.0f", startOffset.Seconds()))
		} else {
			// Flag like "--start=" gets value appended directly
			args = append(args, fmt.Sprintf("%s%.0f", l.startFlag, startOffset.Seconds()))
		}
	} else if startOffset > 0 && l.startFlag == "" {
		l.logger.Warn("cannot set start offset - unknown player, configure start_flag in config",
			"command", l.command, "offset", startOffset)
	}

	l.logger.Info("launching player", "command", l.command, "args", args, "url", url)

	var cmd *exec.Cmd

	// On macOS, try to launch GUI apps with 'open -a' if command not in PATH
	if runtime.GOOS == "darwin" {
		if _, err := exec.LookPath(l.command); err != nil {
			// Look up player config to get proper openFlags
			openFlags := []string{}
			base := strings.ToLower(filepath.Base(l.command))
			base = strings.TrimSuffix(base, filepath.Ext(base))
			if playerCfg, ok := players[base]; ok {
				if paths, ok := playerCfg.platforms["darwin"]; ok {
					// Find the open-a path and use its flags
					for _, lp := range paths {
						if strings.HasPrefix(lp.path, "open-a:") {
							openFlags = lp.openFlags
							break
						}
					}
				}
			}

			// Copy openFlags to avoid modifying the original slice
			cmdArgs := make([]string, len(openFlags))
			copy(cmdArgs, openFlags)

			cmdArgs = append(cmdArgs, "-a", l.command)
			if len(args) > 0 {
				cmdArgs = append(cmdArgs, "--args")
				cmdArgs = append(cmdArgs, args...)
			}
			cmdArgs = append(cmdArgs, url) // URL at the end
			l.logger.Info("using macOS 'open -a' to launch GUI app", "app", l.command, "args", cmdArgs)
			cmd = exec.Command("open", cmdArgs...)
			return cmd.Start()
		}
	}

	// For direct command execution, URL goes at the end
	args = append(args, url)
	cmd = exec.Command(l.command, args...)
	return cmd.Start()
}

// launchDefault opens the URL using the system default handler
func (l *Launcher) launchDefault(url string) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", url)
	default:
		// Linux and other Unix-like systems
		cmd = exec.Command("xdg-open", url)
	}

	l.logger.Info("launching with system default", "os", runtime.GOOS, "url", url)

	return cmd.Start()
}
