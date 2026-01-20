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

// knownPlayerOffsetFlags maps player basenames to their start offset flag
var knownPlayerOffsetFlags = map[string]string{
	"mpv":       "--start=",
	"vlc":       "--start-time=",
	"cvlc":      "--start-time=",
	"mplayer":   "-ss ",
	"mplayer2":  "-ss ",
	"ffplay":    "-ss ",
	"iina":      "--mpv-start=",
	"celluloid": "--mpv-start=",
	"haruna":    "--mpv-start=", // KDE mpv frontend
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

		if flag, ok := knownPlayerOffsetFlags[base]; ok {
			resolvedFlag = flag
			logger.Debug("auto-detected player offset flag", "player", base, "flag", flag)
		}
	}

	return &Launcher{
		command:   command,
		args:      args,
		startFlag: resolvedFlag,
		logger:    logger,
	}
}

// Launch opens a media URL in the configured player or system default
func (l *Launcher) Launch(url string, startOffset time.Duration) error {
	if l.command != "" {
		return l.launchConfigured(url, startOffset)
	}
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

	args = append(args, url)

	l.logger.Info("launching player", "command", l.command, "args", args)

	cmd := exec.Command(l.command, args...)
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
