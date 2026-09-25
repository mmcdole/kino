package player

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Launcher launches media URLs in an external player
type Launcher struct {
	command       string   // configured player command, empty for system default
	args          []string // additional arguments for the player
	seekFlag      string   // user-configured seek flag (e.g., "--start=%d"), overrides table lookup
	logger        *slog.Logger
	detectMu      sync.Mutex
	detected      ResolvedPlayer
	detectedFound bool
	detectedAt    time.Time
}

func lookPathOK(binary string) bool {
	_, err := exec.LookPath(binary)
	return err == nil
}

// tokenParamRe matches credential query parameters in stream URLs
var tokenParamRe = regexp.MustCompile(`(?i)((?:api_key|X-Plex-Token)=)[^&\s"']+`)

// redactTokens masks credential query parameters before anything containing
// a stream URL reaches the log file.
func redactTokens(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = tokenParamRe.ReplaceAllString(a, "${1}REDACTED")
	}
	return out
}

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
func (l *Launcher) Launch(ctx context.Context, url string, startOffset time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	offsetSecs := int(startOffset.Seconds())

	// Tier 1: User configured a specific player
	if l.command != "" {
		l.logger.Info("using configured player", "command", l.command)
		return l.launchConfigured(url, offsetSecs)
	}

	// Tier 2: Auto-detect known players
	player, found := l.detectPlayer(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	if found {
		l.logger.Info("auto-detected player", "binary", player.Definition.Binary,
			"executable", player.Executable)
		if err := l.execPlayer(player, url, offsetSecs); err != nil {
			// Do not pin a stale/broken executable for the rest of the process.
			l.invalidateDetectedPlayer()
			return err
		}
		return nil
	}

	// Tier 3: Best-effort system URL handler fallback. For HTTP media URLs this
	// is normally a browser, whose container/codec support is more limited than
	// a real media player's.
	l.logger.Warn("no video players found, opening raw media URL with system handler; codec support may be limited")
	if offsetSecs > 0 {
		l.logger.Warn("resume not supported with system default player - starting from beginning")
	}
	return l.launchDefault(url)
}

// execPlayer launches the detected player with optional seek offset
func (l *Launcher) execPlayer(player ResolvedPlayer, url string, offsetSecs int) error {
	args := playerArgs(player.Definition, url, offsetSecs)

	l.logger.Debug("executing player", "binary", player.Definition.Binary,
		"executable", player.Executable, "args", redactTokens(args))
	return startCommand(exec.Command(player.Executable, args...))
}

func playerArgs(player PlayerDef, url string, offsetSecs int) []string {
	seekArgs := formatSeekArgs(player.SeekFlag, offsetSecs)
	var defaultArgs []string
	if offsetSecs > 0 {
		defaultArgs = player.ResumeArgs
	}
	if player.URLBeforeSeek {
		args := append([]string{url}, defaultArgs...)
		return append(args, seekArgs...)
	}
	args := append([]string{}, defaultArgs...)
	args = append(args, seekArgs...)
	return append(args, url)
}

func formatSeekArgs(flag string, offsetSecs int) []string {
	flag = strings.TrimSpace(flag)
	if flag == "" || offsetSecs <= 0 {
		return nil
	}

	offset := strconv.Itoa(offsetSecs)
	switch {
	case strings.Contains(flag, "%d"):
		flag = strings.ReplaceAll(flag, "%d", offset)
	case strings.HasSuffix(flag, "="):
		// Backward-compatible with the originally documented "--start=" form.
		flag += offset
	default:
		flag += " " + offset
	}
	return strings.Fields(flag)
}

// launchConfigured launches the media using the user-configured player
func (l *Launcher) launchConfigured(url string, offsetSecs int) error {
	args := append([]string{}, l.args...)
	definition, knownPlayer := l.lookupPlayerDef(l.command)
	var seekArgs []string

	// Add seek offset: user-configured flag takes precedence, then table lookup
	if offsetSecs > 0 {
		seekFlag := l.seekFlag
		if seekFlag == "" {
			// Fall back to table lookup for known players
			seekFlag = definition.SeekFlag
		}

		if seekFlag != "" {
			seekArgs = formatSeekArgs(seekFlag, offsetSecs)
		} else {
			l.logger.Warn("cannot set start offset - unknown player, configure start_flag in config",
				"command", l.command, "offset", offsetSecs)
		}
	}

	args = configuredPlayerArgs(args, definition, knownPlayer, url, seekArgs)

	l.logger.Debug("launching configured player", "command", l.command, "args", redactTokens(args))

	// On macOS, try 'open -a' if command not in PATH (for GUI apps)
	if runtime.GOOS == "darwin" {
		if _, err := exec.LookPath(l.command); err != nil {
			return l.launchMacOSApp(l.command, args)
		}
	}

	return startCommand(exec.Command(l.command, args...))
}

func configuredPlayerArgs(configuredArgs []string, definition PlayerDef, knownPlayer bool, url string, seekArgs []string) []string {
	if knownPlayer && definition.URLBeforeSeek {
		args := append([]string{url}, configuredArgs...)
		return append(args, seekArgs...)
	}
	args := append([]string{}, configuredArgs...)
	args = append(args, seekArgs...)
	return append(args, url)
}

func (l *Launcher) lookupPlayerDef(binary string) (PlayerDef, bool) {
	wanted := executableName(binary)
	for _, table := range [][]PlayerDef{linuxPlayers, darwinPlayers, windowsPlayers} {
		for _, p := range table {
			if strings.EqualFold(executableName(p.Binary), wanted) {
				return p, true
			}
		}
	}
	return PlayerDef{}, false
}

func executableName(command string) string {
	command = strings.Trim(strings.TrimSpace(command), `"`)
	command = strings.ReplaceAll(command, `\`, "/")
	if index := strings.LastIndex(command, "/"); index >= 0 {
		return command[index+1:]
	}
	return command
}

// launchMacOSApp launches a macOS GUI app using 'open -a'
func (l *Launcher) launchMacOSApp(appName string, playerArgs []string) error {
	cmdArgs := []string{"-a", appName}
	if len(playerArgs) > 0 {
		cmdArgs = append(cmdArgs, "--args")
		cmdArgs = append(cmdArgs, playerArgs...)
	}

	l.logger.Debug("using macOS 'open -a'", "app", appName, "args", redactTokens(cmdArgs))
	return startCommand(exec.Command("open", cmdArgs...))
}

// launchDefault opens the URL using the system default handler
func (l *Launcher) launchDefault(url string) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		switch {
		case lookPathOK("rundll32.exe"):
			cmd = exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", url)
		case lookPathOK("explorer.exe"):
			cmd = exec.Command("explorer.exe", url)
		}
	default:
		// Linux and other Unix-like systems
		if isWSL() {
			// WSL distros usually have no xdg-open; hand the URL to Windows.
			// wslview (from wslu) is purpose-built for this. rundll32's
			// FileProtocolHandler is the reliable built-in: explorer.exe
			// chokes on URLs with query strings (?a=1&b=2) and silently
			// opens the Documents folder instead.
			switch {
			case lookPathOK("wslview"):
				cmd = exec.Command("wslview", url)
			case lookPathOK("rundll32.exe"):
				cmd = exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", url)
			case lookPathOK("explorer.exe"):
				cmd = exec.Command("explorer.exe", url)
			}
		}
		if cmd == nil {
			if _, err := exec.LookPath("xdg-open"); err != nil {
				return fmt.Errorf("no media player found — install mpv (or vlc), or set player.command in config.yaml")
			}
			cmd = exec.Command("xdg-open", url)
		}
	}
	if cmd == nil {
		return fmt.Errorf("no media player or system URL handler found — install mpv (or vlc), or set player.command in config.yaml")
	}

	l.logger.Debug("launching with system default", "os", runtime.GOOS, "command", cmd.Path)
	return startCommand(cmd)
}

// startCommand always reaps the child process. Player processes may live for a
// long time, so waiting happens asynchronously rather than blocking the TUI.
func startCommand(cmd *exec.Cmd) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() {
		_ = cmd.Wait()
	}()
	return nil
}
