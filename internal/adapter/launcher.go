package adapter

import (
	"fmt"
	"log/slog"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Launcher launches media URLs in an external player
type Launcher struct {
	command  string   // configured player command, empty for system default
	args     []string // additional arguments for the player
	seekFlag string   // user-configured seek flag (e.g., "--start=%d"), overrides table lookup
	logger   *slog.Logger
}

// PlayerDef defines a player binary and its seek flag format
type PlayerDef struct {
	Binary   string
	SeekFlag string // Use %d for seconds placeholder, e.g., "--start=%d" or "-ss %d"
}

// Platform-specific player lists, ordered by priority (first match wins)
var linuxPlayers = []PlayerDef{
	{Binary: "mpv", SeekFlag: "--start=%d"},
	{Binary: "vlc", SeekFlag: "--start-time=%d"},
	{Binary: "celluloid", SeekFlag: "--mpv-start=%d"},
	{Binary: "haruna", SeekFlag: "--start=%d"},
	{Binary: "smplayer", SeekFlag: "-ss %d"},
	{Binary: "mplayer", SeekFlag: "-ss %d"},
}

var darwinPlayers = []PlayerDef{
	{Binary: "iina", SeekFlag: "--mpv-start=%d"},
	{Binary: "mpv", SeekFlag: "--start=%d"},
	{Binary: "vlc", SeekFlag: "--start-time=%d"},
}

// playersByBinary provides lookup for configured player seek flags
var playersByBinary = func() map[string]string {
	m := make(map[string]string)
	for _, p := range linuxPlayers {
		m[p.Binary] = p.SeekFlag
	}
	for _, p := range darwinPlayers {
		m[p.Binary] = p.SeekFlag
	}
	return m
}()

// NewLauncher creates a new Launcher
// seekFlag is optional - if empty, we look up the flag from our known players table
func NewLauncher(command string, args []string, seekFlag string, logger *slog.Logger) *Launcher {
	if logger == nil {
		logger = slog.Default()
	}

	return &Launcher{
		command:  command,
		args:     args,
		seekFlag: seekFlag,
		logger:   logger,
	}
}

// Launch opens a media URL in the configured player or auto-detected player
func (l *Launcher) Launch(url string, startOffset time.Duration) error {
	offsetSecs := int(startOffset.Seconds())

	// Tier 1: User configured a specific player
	if l.command != "" {
		l.logger.Info("using configured player", "command", l.command)
		return l.launchConfigured(url, offsetSecs)
	}

	// Tier 2: Auto-detect known players
	if player, found := l.detectPlayer(); found {
		l.logger.Info("auto-detected player", "binary", player.Binary)
		return l.execPlayer(player, url, offsetSecs)
	}

	// Tier 3: System default fallback (xdg-open/open)
	l.logger.Warn("no video players found, falling back to system default")
	if offsetSecs > 0 {
		l.logger.Warn("resume not supported with system default player - starting from beginning")
	}
	return l.launchDefault(url)
}

// detectPlayer returns the first available player from the platform-specific list
func (l *Launcher) detectPlayer() (PlayerDef, bool) {
	var candidates []PlayerDef

	switch runtime.GOOS {
	case "darwin":
		candidates = darwinPlayers
	case "linux":
		candidates = linuxPlayers
	default:
		return PlayerDef{}, false
	}

	for _, p := range candidates {
		if path, err := exec.LookPath(p.Binary); err == nil && path != "" {
			return p, true
		}
	}
	return PlayerDef{}, false
}

// execPlayer launches the detected player with optional seek offset
func (l *Launcher) execPlayer(player PlayerDef, url string, offsetSecs int) error {
	args := []string{}

	// Add seek flag if we have an offset and the player supports it
	if offsetSecs > 0 && player.SeekFlag != "" {
		formattedFlag := fmt.Sprintf(player.SeekFlag, offsetSecs)
		// Split flags like "-ss 10" into separate args
		args = append(args, strings.Fields(formattedFlag)...)
	}

	args = append(args, url)

	l.logger.Debug("executing player", "binary", player.Binary, "args", args)
	cmd := exec.Command(player.Binary, args...)
	return cmd.Start()
}

// launchConfigured launches the media using the user-configured player
func (l *Launcher) launchConfigured(url string, offsetSecs int) error {
	args := append([]string{}, l.args...)

	// Add seek offset: user-configured flag takes precedence, then table lookup
	if offsetSecs > 0 {
		seekFlag := l.seekFlag
		if seekFlag == "" {
			// Fall back to table lookup for known players
			seekFlag = playersByBinary[l.command]
		}

		if seekFlag != "" {
			formattedFlag := fmt.Sprintf(seekFlag, offsetSecs)
			args = append(args, strings.Fields(formattedFlag)...)
		} else {
			l.logger.Warn("cannot set start offset - unknown player, configure start_flag in config",
				"command", l.command, "offset", offsetSecs)
		}
	}

	args = append(args, url)

	l.logger.Debug("launching configured player", "command", l.command, "args", args)

	// On macOS, try 'open -a' if command not in PATH (for GUI apps)
	if runtime.GOOS == "darwin" {
		if _, err := exec.LookPath(l.command); err != nil {
			return l.launchMacOSApp(l.command, args)
		}
	}

	cmd := exec.Command(l.command, args...)
	return cmd.Start()
}

// launchMacOSApp launches a macOS GUI app using 'open -a'
func (l *Launcher) launchMacOSApp(appName string, playerArgs []string) error {
	cmdArgs := []string{"-a", appName}
	if len(playerArgs) > 0 {
		cmdArgs = append(cmdArgs, "--args")
		cmdArgs = append(cmdArgs, playerArgs...)
	}

	l.logger.Debug("using macOS 'open -a'", "app", appName, "args", cmdArgs)
	cmd := exec.Command("open", cmdArgs...)
	return cmd.Start()
}

// launchDefault opens the URL using the system default handler
func (l *Launcher) launchDefault(url string) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		// Linux and other Unix-like systems
		cmd = exec.Command("xdg-open", url)
	}

	l.logger.Debug("launching with system default", "os", runtime.GOOS, "url", url)
	return cmd.Start()
}
