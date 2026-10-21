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

// launcher launches media URLs in an external player
type launcher struct {
	command       string   // configured player command, empty for system default
	args          []string // additional arguments for the player
	seekFlag      string   // user-configured seek flag (e.g., "--start=%d"), overrides table lookup
	logger        *slog.Logger
	detectMu      sync.Mutex
	detected      resolvedPlayer
	detectedFound bool
	detectedAt    time.Time
}

func lookPathOK(binary string) bool {
	_, err := exec.LookPath(binary)
	return err == nil
}

// tokenParamRe matches credential query parameters in stream URLs
var tokenParamRe = regexp.MustCompile(`(?i)((?:api_key|X-Plex-Token)=)[^&\s"']+`)

// playPathRe matches the play ID in a Kivo media URL, which is its credential
var playPathRe = regexp.MustCompile(`(/plays/)[^/\s"']+`)

// redactTokens masks credentials in stream URLs before anything containing
// one reaches the log file.
func redactTokens(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		a = tokenParamRe.ReplaceAllString(a, "${1}REDACTED")
		out[i] = playPathRe.ReplaceAllString(a, "${1}REDACTED")
	}
	return out
}

// newLauncher returns a launcher for command, or for the system default.
// seekFlag is optional - if empty, we look up the flag from our known players table
func newLauncher(command string, args []string, seekFlag string, logger *slog.Logger) *launcher {
	if logger == nil {
		logger = slog.Default()
	}

	return &launcher{
		command:  command,
		args:     args,
		seekFlag: seekFlag,
		logger:   logger,
	}
}

// Launch opens a media URL in the configured player or auto-detected player.
// The player outlives the call; nothing waits on it.
func (l *launcher) Launch(ctx context.Context, url string, startOffset time.Duration) error {
	p, err := l.start(ctx, launchSpec{urls: []string{url}, offset: startOffset})
	if err != nil {
		return err
	}
	p.release()
	return nil
}

// launchSpec is what one launch opens.
type launchSpec struct {
	urls   []string      // played in order
	offset time.Duration // where to start in urls[0]
	ipc    string        // socket for the player to listen on, when it can
}

// running is a launched player.
type running struct {
	cmd *exec.Cmd // nil when the player's lifetime can't be followed (a system handler, open -a)
	ipc bool      // whether the player was told to listen on the spec's socket
}

// wait blocks until the player exits. It reports false when there is no
// process to wait on.
func (r running) wait() bool {
	if r.cmd == nil {
		return false
	}
	_ = r.cmd.Wait()
	return true
}

// release reaps the player in the background, for a caller that won't wait.
func (r running) release() {
	if r.cmd != nil {
		go func() { _ = r.cmd.Wait() }()
	}
}

// start launches the configured, auto-detected or system player.
func (l *launcher) start(ctx context.Context, spec launchSpec) (running, error) {
	if err := ctx.Err(); err != nil {
		return running{}, err
	}
	offsetSecs := int(spec.offset.Seconds())

	// Tier 1: User configured a specific player
	if l.command != "" {
		l.logger.Info("using configured player", "command", l.command)
		return l.launchConfigured(spec, offsetSecs)
	}

	// Tier 2: Auto-detect known players
	player, found := l.detectPlayer(ctx)
	if err := ctx.Err(); err != nil {
		return running{}, err
	}
	if found {
		l.logger.Info("auto-detected player", "binary", player.Definition.Binary,
			"executable", player.Executable)
		p, err := l.execPlayer(player, spec, offsetSecs)
		if err != nil {
			// Do not pin a stale/broken executable for the rest of the process.
			l.invalidateDetectedPlayer()
			return running{}, err
		}
		return p, nil
	}

	// Tier 3: Best-effort system URL handler fallback. For HTTP media URLs this
	// is normally a browser, whose container/codec support is more limited than
	// a real media player's.
	l.logger.Warn("no video players found, opening raw media URL with system handler; codec support may be limited")
	if offsetSecs > 0 {
		l.logger.Warn("resume not supported with system default player - starting from beginning")
	}
	return running{}, l.launchDefault(spec.urls[0])
}

// execPlayer launches the detected player with optional seek offset
func (l *launcher) execPlayer(player resolvedPlayer, spec launchSpec, offsetSecs int) (running, error) {
	media, ipc := mediaArgs(player.Definition, spec, player.Executable)
	args := playerArgs(player.Definition, media, offsetSecs, len(spec.urls))

	l.logger.Debug("executing player", "binary", player.Definition.Binary,
		"executable", player.Executable, "args", redactTokens(args))
	cmd, err := startCommand(exec.Command(player.Executable, args...))
	return running{cmd: cmd, ipc: ipc}, err
}

// mediaArgs returns the arguments that open spec's URLs, and whether they
// ask the player to listen on spec's IPC socket. Several URLs play in order;
// mpv starts only the first at the offset (its own --start applies to every
// file), which playerArgs then leaves out.
func mediaArgs(player playerDef, spec launchSpec, executable string) ([]string, bool) {
	var args []string
	ipc := spec.ipc != "" && player.IPCFlag != "" && !strings.HasSuffix(strings.ToLower(executable), ".exe")
	if ipc {
		args = append(args, fmt.Sprintf(player.IPCFlag, spec.ipc))
	}
	if perFileStart(player, spec) {
		args = append(args, "--{", "--start="+strconv.Itoa(int(spec.offset.Seconds())), spec.urls[0], "--}")
		return append(args, spec.urls[1:]...), ipc
	}
	return append(args, spec.urls...), ipc
}

// perFileStart reports a multi-part start that only mpv's per-file options
// can express.
func perFileStart(player playerDef, spec launchSpec) bool {
	return len(spec.urls) > 1 && spec.offset >= time.Second && player.PerFileOptions
}

// playerArgs places the seek switch around media, the arguments that open
// the stream. parts is how many URLs media holds.
func playerArgs(player playerDef, media []string, offsetSecs int, parts int) []string {
	if parts > 1 && player.PerFileOptions {
		offsetSecs = 0 // mediaArgs started the first part itself
	}
	seekArgs := formatSeekArgs(player.SeekFlag, offsetSecs)
	var defaultArgs []string
	if offsetSecs > 0 {
		defaultArgs = player.ResumeArgs
	}
	if player.URLBeforeSeek {
		args := append(append([]string{}, media...), defaultArgs...)
		return append(args, seekArgs...)
	}
	args := append([]string{}, defaultArgs...)
	args = append(args, seekArgs...)
	return append(args, media...)
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
func (l *launcher) launchConfigured(spec launchSpec, offsetSecs int) (running, error) {
	args := append([]string{}, l.args...)
	definition, knownPlayer := l.lookupPlayerDef(l.command)
	media, ipc := mediaArgs(definition, spec, l.command)
	if perFileStart(definition, spec) {
		offsetSecs = 0
	}
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

	args = configuredPlayerArgs(args, definition, knownPlayer, media, seekArgs)

	l.logger.Debug("launching configured player", "command", l.command, "args", redactTokens(args))

	// On macOS, try 'open -a' if command not in PATH (for GUI apps)
	if runtime.GOOS == "darwin" {
		if _, err := exec.LookPath(l.command); err != nil {
			return running{ipc: ipc}, l.launchMacOSApp(l.command, args)
		}
	}

	cmd, err := startCommand(exec.Command(l.command, args...))
	return running{cmd: cmd, ipc: ipc}, err
}

func configuredPlayerArgs(configuredArgs []string, definition playerDef, knownPlayer bool, media []string, seekArgs []string) []string {
	if knownPlayer && definition.URLBeforeSeek {
		args := append(append([]string{}, media...), configuredArgs...)
		return append(args, seekArgs...)
	}
	args := append([]string{}, configuredArgs...)
	args = append(args, seekArgs...)
	return append(args, media...)
}

func (l *launcher) lookupPlayerDef(binary string) (playerDef, bool) {
	wanted := executableName(binary)
	for _, table := range [][]playerDef{linuxPlayers, darwinPlayers, windowsPlayers} {
		for _, p := range table {
			if strings.EqualFold(executableName(p.Binary), wanted) {
				return p, true
			}
		}
	}
	return playerDef{}, false
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
func (l *launcher) launchMacOSApp(appName string, playerArgs []string) error {
	cmdArgs := []string{"-a", appName}
	if len(playerArgs) > 0 {
		cmdArgs = append(cmdArgs, "--args")
		cmdArgs = append(cmdArgs, playerArgs...)
	}

	l.logger.Debug("using macOS 'open -a'", "app", appName, "args", redactTokens(cmdArgs))
	cmd, err := startCommand(exec.Command("open", cmdArgs...))
	if err == nil {
		running{cmd: cmd}.release() // open exits at once; the app is not its child
	}
	return err
}

// launchDefault opens the URL using the system default handler
func (l *launcher) launchDefault(url string) error {
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
	if _, err := startCommand(cmd); err != nil {
		return err
	}
	running{cmd: cmd}.release()
	return nil
}

// startCommand starts cmd. The caller reaps it: a player may live for a long
// time, so only a play session waits on it, and everyone else releases it.
func startCommand(cmd *exec.Cmd) (*exec.Cmd, error) {
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}
