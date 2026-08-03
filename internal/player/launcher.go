package player

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mmcdole/kino/internal/domain"
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

// PlayerDef defines a player binary and its seek flag format
type PlayerDef struct {
	Binary        string
	SeekFlag      string   // Use %d for seconds placeholder, e.g., "--start=%d" or "-ss %d"
	URLBeforeSeek bool     // Some players (notably PotPlayer) expect the media URL before switches
	ProgramPaths  []string // Conventional paths relative to Windows Program Files roots
}

// ResolvedPlayer keeps the player identity separate from the executable path.
// This matters in WSL, where an App Paths lookup returns an absolute Windows
// install path rather than the short binary name used to select seek behavior.
type ResolvedPlayer struct {
	Definition PlayerDef
	Executable string
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

// Windows players, used both by native Windows and WSL interop. Under WSL they
// are probed after the native Linux list so an intentional WSLg install wins.
var windowsPlayers = []PlayerDef{
	{
		Binary:        "PotPlayerMini64.exe",
		SeekFlag:      "/seek=%d",
		URLBeforeSeek: true,
		ProgramPaths:  []string{`DAUM\PotPlayer\PotPlayerMini64.exe`},
	},
	{
		Binary:        "PotPlayerMini.exe",
		SeekFlag:      "/seek=%d",
		URLBeforeSeek: true,
		ProgramPaths:  []string{`DAUM\PotPlayer\PotPlayerMini.exe`},
	},
	{Binary: "mpv.exe", SeekFlag: "--start=%d", ProgramPaths: []string{`mpv\mpv.exe`}},
	{Binary: "vlc.exe", SeekFlag: "--start-time=%d", ProgramPaths: []string{`VideoLAN\VLC\vlc.exe`}},
}

// Kept as an alias because WSL uses the same Windows-side player definitions.
var wslPlayers = windowsPlayers

var windowsAppPathRoots = []string{
	`HKCU\SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths`,
	`HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths`,
	`HKLM\SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\App Paths`,
}

const (
	windowsDiscoveryTimeout = 5 * time.Second
	negativeDetectionTTL    = 30 * time.Second
)

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

// isWSL reports whether we are running inside Windows Subsystem for Linux.
func isWSL() bool {
	if os.Getenv("WSL_DISTRO_NAME") != "" || os.Getenv("WSL_INTEROP") != "" {
		return true
	}
	data, err := os.ReadFile("/proc/sys/kernel/osrelease")
	return err == nil && strings.Contains(strings.ToLower(string(data)), "microsoft")
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
func (l *Launcher) Launch(url string, startOffset time.Duration) error {
	offsetSecs := int(startOffset.Seconds())

	// Tier 1: User configured a specific player
	if l.command != "" {
		l.logger.Info("using configured player", "command", l.command)
		return l.launchConfigured(url, offsetSecs)
	}

	// Tier 2: Auto-detect known players
	if player, found := l.detectPlayer(); found {
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

// detectPlayer returns the first available player from the platform-specific list
func (l *Launcher) detectPlayer() (ResolvedPlayer, bool) {
	l.detectMu.Lock()
	defer l.detectMu.Unlock()

	if l.detectedFound {
		return l.detected, true
	}
	if !l.detectedAt.IsZero() && time.Since(l.detectedAt) < negativeDetectionTTL {
		return ResolvedPlayer{}, false
	}

	l.detected, l.detectedFound = detectPlayerUncached()
	l.detectedAt = time.Now()
	return l.detected, l.detectedFound
}

func (l *Launcher) invalidateDetectedPlayer() {
	l.detectMu.Lock()
	defer l.detectMu.Unlock()
	l.detected = ResolvedPlayer{}
	l.detectedFound = false
	l.detectedAt = time.Time{}
}

func detectPlayerUncached() (ResolvedPlayer, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), windowsDiscoveryTimeout)
	defer cancel()

	var candidates []PlayerDef
	underWSL := runtime.GOOS == "linux" && isWSL()
	searchWindowsInstalls := false

	switch runtime.GOOS {
	case "darwin":
		candidates = darwinPlayers
	case "linux":
		candidates = linuxPlayers
		if underWSL {
			// WSL can execute Windows binaries via interop; a Windows-side
			// player on PATH is a perfectly good player.
			candidates = append(append([]PlayerDef{}, linuxPlayers...), wslPlayers...)
			searchWindowsInstalls = true
		}
	case "windows":
		candidates = windowsPlayers
		searchWindowsInstalls = true
	default:
		return ResolvedPlayer{}, false
	}

	// Honor PATH first. In WSL this also ensures an explicitly exposed
	// Windows player wins over a different player merely found in the registry.
	for _, p := range candidates {
		if path, err := exec.LookPath(p.Binary); err == nil && path != "" {
			return ResolvedPlayer{Definition: p, Executable: path}, true
		}
	}

	// CreateProcess/exec.LookPath does not consult Windows "App Paths". GUI
	// installers commonly register there without adding themselves to PATH, so
	// query it explicitly on native Windows and through interop on WSL.
	if searchWindowsInstalls {
		for _, p := range windowsPlayers {
			if executable, found := resolveWindowsAppPath(ctx, p, underWSL); found {
				return ResolvedPlayer{Definition: p, Executable: executable}, true
			}
		}

		// App Paths is the authoritative registration mechanism. Only after all
		// registered candidates miss do we try conventional install locations.
		programFilesRoots := windowsProgramFilesRoots(ctx, underWSL)
		for _, p := range windowsPlayers {
			if executable, found := resolveWindowsProgramPath(ctx, p, programFilesRoots, underWSL); found {
				return ResolvedPlayer{Definition: p, Executable: executable}, true
			}
		}
	}

	return ResolvedPlayer{}, false
}

func resolveWindowsAppPath(ctx context.Context, player PlayerDef, underWSL bool) (string, bool) {
	for _, root := range windowsAppPathRoots {
		key := root + `\` + player.Binary
		if windowsPath, found := queryRegistryValue(ctx, key, "/ve"); found {
			if executable, ok := usableWindowsExecutable(ctx, windowsPath, underWSL); ok {
				return executable, true
			}
		}
	}
	return "", false
}

// resolveWindowsProgramPath checks a short list of conventional locations. It
// deliberately avoids recursively scanning a mounted Windows drive.
func resolveWindowsProgramPath(ctx context.Context, player PlayerDef, roots []string, underWSL bool) (string, bool) {
	for _, root := range roots {
		for _, relative := range player.ProgramPaths {
			windowsPath := strings.TrimRight(root, `\/`) + `\` + strings.TrimLeft(relative, `\/`)
			if executable, ok := usableWindowsExecutable(ctx, windowsPath, underWSL); ok {
				return executable, true
			}
		}
	}

	return "", false
}

func queryRegistryValue(ctx context.Context, key string, valueArgs ...string) (string, bool) {
	if !lookPathOK("reg.exe") {
		return "", false
	}

	args := append([]string{"query", key}, valueArgs...)
	output, err := exec.CommandContext(ctx, "reg.exe", args...).Output()
	if err != nil {
		return "", false
	}
	return parseRegistryString(output)
}

func parseRegistryString(output []byte) (string, bool) {
	for _, line := range strings.Split(string(output), "\n") {
		for _, valueType := range []string{"REG_EXPAND_SZ", "REG_SZ"} {
			if index := strings.Index(line, valueType); index >= 0 {
				value := cleanWindowsExecutable(line[index+len(valueType):])
				return value, value != ""
			}
		}
	}
	return "", false
}

func cleanWindowsExecutable(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, `"`) {
		if end := strings.Index(value[1:], `"`); end >= 0 {
			return value[1 : end+1]
		}
	}
	return strings.Trim(value, `"`)
}

func usableWindowsExecutable(ctx context.Context, windowsPath string, underWSL bool) (string, bool) {
	windowsPath = expandWindowsEnvironment(ctx, windowsPath, underWSL)
	executable := windowsPath
	if underWSL {
		output, err := exec.CommandContext(ctx, "wslpath", "-u", windowsPath).Output()
		if err != nil {
			return "", false
		}
		executable = strings.TrimSpace(string(output))
	}

	info, err := os.Stat(executable)
	return executable, err == nil && !info.IsDir()
}

var windowsEnvironmentReference = regexp.MustCompile(`%[A-Za-z0-9_()]+%`)

func expandWindowsEnvironment(ctx context.Context, value string, underWSL bool) string {
	return windowsEnvironmentReference.ReplaceAllStringFunc(value, func(reference string) string {
		name := reference[1 : len(reference)-1]
		if !underWSL {
			if expanded, found := os.LookupEnv(name); found {
				return expanded
			}
			return reference
		}

		if !lookPathOK("cmd.exe") {
			return reference
		}
		output, err := exec.CommandContext(ctx, "cmd.exe", "/d", "/s", "/c", "echo "+reference).Output()
		if err != nil {
			return reference
		}
		expanded := strings.TrimSpace(string(output))
		if expanded == "" || strings.EqualFold(expanded, reference) {
			return reference
		}
		return expanded
	})
}

func windowsProgramFilesRoots(ctx context.Context, underWSL bool) []string {
	if !underWSL {
		return uniqueNonEmpty(os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"))
	}

	const currentVersionKey = `HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion`
	var roots []string
	for _, valueName := range []string{"ProgramFilesDir", "ProgramFilesDir (x86)"} {
		if root, found := queryRegistryValue(ctx, currentVersionKey, "/v", valueName); found {
			roots = append(roots, root)
		}
	}
	return uniqueNonEmpty(roots...)
}

func uniqueNonEmpty(values ...string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		key := strings.ToLower(value)
		if value == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
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
	defaultArgs := playerDefaultArgs(player, offsetSecs)
	if player.URLBeforeSeek {
		args := append([]string{url}, defaultArgs...)
		return append(args, seekArgs...)
	}
	args := append([]string{}, defaultArgs...)
	args = append(args, seekArgs...)
	return append(args, url)
}

func playerDefaultArgs(player PlayerDef, offsetSecs int) []string {
	// A running VLC instance can ignore --start-time for a subsequently opened
	// URL. Keeping auto-detected playback in its own instance makes resume
	// deterministic; explicitly configured VLC remains under user control.
	if offsetSecs > 0 && (strings.EqualFold(player.Binary, "vlc.exe") || strings.EqualFold(player.Binary, "vlc")) {
		return []string{"--no-one-instance"}
	}
	return nil
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

// lookupSeekFlag finds the seek flag for a known player binary
func (l *Launcher) lookupSeekFlag(binary string) string {
	player, _ := l.lookupPlayerDef(binary)
	return player.SeekFlag
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

// Service orchestrates playback operations
type Service struct {
	launcher *Launcher
	playback domain.PlaybackClient
	logger   *slog.Logger
}

// NewService creates a new playback service
func NewService(launcher *Launcher, playback domain.PlaybackClient, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		launcher: launcher,
		playback: playback,
		logger:   logger,
	}
}

// Play starts playback of a media item from the beginning
func (s *Service) Play(ctx context.Context, item domain.MediaItem) error {
	return s.playItem(ctx, item, 0)
}

// Resume starts playback from the saved position
func (s *Service) Resume(ctx context.Context, item domain.MediaItem) error {
	return s.playItem(ctx, item, item.ViewOffset)
}

// playItem resolves URL and launches player
func (s *Service) playItem(ctx context.Context, item domain.MediaItem, offset time.Duration) error {
	url, err := s.playback.ResolvePlayableURL(ctx, item.ID)
	if err != nil {
		s.logger.Error("failed to resolve playable URL", "error", err, "itemID", item.ID)
		return err
	}

	s.logger.Info("launching playback", "title", item.Title, "itemID", item.ID, "offset", offset)

	return s.launcher.Launch(url, offset)
}

// MarkWatched marks an item as fully watched
func (s *Service) MarkWatched(ctx context.Context, itemID string) error {
	return s.playback.MarkPlayed(ctx, itemID)
}

// MarkUnwatched marks an item as unwatched
func (s *Service) MarkUnwatched(ctx context.Context, itemID string) error {
	return s.playback.MarkUnplayed(ctx, itemID)
}
