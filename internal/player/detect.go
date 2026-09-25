package player

import (
	"context"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// PlayerDef defines a platform-specific player command and playback arguments.
type PlayerDef struct {
	Binary        string
	SeekFlag      string   // Use %d for seconds placeholder, e.g., "--start=%d" or "-ss %d"
	ResumeArgs    []string // Additional auto-detection arguments used only when resuming
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

// Platform-specific player lists, ordered by priority (first match wins).
// Linux and Windows VLC need a separate instance to honor resume offsets.
// macOS VLC does not support the one-instance option.
var linuxPlayers = []PlayerDef{
	{Binary: "mpv", SeekFlag: "--start=%d"},
	{Binary: "vlc", SeekFlag: "--start-time=%d", ResumeArgs: []string{"--no-one-instance"}},
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
	{Binary: "vlc.exe", SeekFlag: "--start-time=%d", ResumeArgs: []string{"--no-one-instance"}, ProgramPaths: []string{`VideoLAN\VLC\vlc.exe`}},
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

// isWSL reports whether we are running inside Windows Subsystem for Linux.
func isWSL() bool {
	if os.Getenv("WSL_DISTRO_NAME") != "" || os.Getenv("WSL_INTEROP") != "" {
		return true
	}
	data, err := os.ReadFile("/proc/sys/kernel/osrelease")
	return err == nil && strings.Contains(strings.ToLower(string(data)), "microsoft")
}

// detectPlayer returns the first available player from the platform-specific list
func (l *Launcher) detectPlayer(ctx context.Context) (ResolvedPlayer, bool) {
	l.detectMu.Lock()
	defer l.detectMu.Unlock()

	if l.detectedFound {
		return l.detected, true
	}
	if !l.detectedAt.IsZero() && time.Since(l.detectedAt) < negativeDetectionTTL {
		return ResolvedPlayer{}, false
	}

	l.detected, l.detectedFound = detectPlayerUncached(ctx)
	if ctx.Err() != nil {
		return ResolvedPlayer{}, false
	}
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

func detectPlayerUncached(parent context.Context) (ResolvedPlayer, bool) {
	ctx, cancel := context.WithTimeout(parent, windowsDiscoveryTimeout)
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
