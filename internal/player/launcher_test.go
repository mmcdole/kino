package player

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func fakeBinary(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	return path
}

func forceWSL(t *testing.T) {
	t.Helper()
	t.Setenv("WSL_DISTRO_NAME", "Ubuntu")
}

// Under WSL, a Windows-side mpv.exe on PATH (via interop) must be detected
// when no Linux player is installed.
func TestDetectPlayerWSLFindsExe(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only")
	}
	dir := t.TempDir()
	fakeBinary(t, dir, "mpv.exe")
	t.Setenv("PATH", dir)
	forceWSL(t)

	l := NewLauncher("", nil, "", nil)
	p, found := l.detectPlayer()
	if !found {
		t.Fatal("mpv.exe not detected under WSL")
	}
	if p.Definition.Binary != "mpv.exe" {
		t.Fatalf("detected %q, want mpv.exe", p.Definition.Binary)
	}
	if p.Definition.SeekFlag != "--start=%d" {
		t.Fatalf("wrong seek flag %q", p.Definition.SeekFlag)
	}
}

// PotPlayer outranks other Windows players when several are installed.
func TestDetectPlayerWSLPotPlayerFirst(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only")
	}
	dir := t.TempDir()
	fakeBinary(t, dir, "mpv.exe")
	fakeBinary(t, dir, "PotPlayerMini64.exe")
	t.Setenv("PATH", dir)
	forceWSL(t)

	l := NewLauncher("", nil, "", nil)
	p, found := l.detectPlayer()
	if !found || p.Definition.Binary != "PotPlayerMini64.exe" {
		t.Fatalf("expected PotPlayer to win, got %q (found=%v)", p.Definition.Binary, found)
	}
	if p.Definition.SeekFlag != "/seek=%d" {
		t.Fatalf("wrong PotPlayer seek flag %q", p.Definition.SeekFlag)
	}
}

// A native Linux player still wins over the Windows-side one.
func TestDetectPlayerWSLPrefersLinuxPlayer(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only")
	}
	dir := t.TempDir()
	fakeBinary(t, dir, "mpv")
	fakeBinary(t, dir, "mpv.exe")
	t.Setenv("PATH", dir)
	forceWSL(t)

	l := NewLauncher("", nil, "", nil)
	p, found := l.detectPlayer()
	if !found || p.Definition.Binary != "mpv" {
		t.Fatalf("expected native mpv to win, got %q (found=%v)", p.Definition.Binary, found)
	}
}

// Windows GUI installers commonly register App Paths without adding their
// directory to PATH. WSL discovery must resolve and convert that path.
func TestDetectPlayerWSLFindsWindowsAppPath(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only")
	}

	dir := t.TempDir()
	installed := fakeBinary(t, dir, "installed-vlc.exe")
	regScript := `#!/bin/sh
case "$2" in
  *vlc.exe) printf '\nHKEY_LOCAL_MACHINE\\...\\vlc.exe\n    (Default)    REG_SZ    C:\\Program Files\\VideoLAN\\VLC\\vlc.exe\n' ;;
  *) exit 1 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "reg.exe"), []byte(regScript), 0755); err != nil {
		t.Fatal(err)
	}
	wslpathScript := `#!/bin/sh
[ "$1" = "-u" ] || exit 1
[ "$2" = 'C:\Program Files\VideoLAN\VLC\vlc.exe' ] || exit 1
printf '%s\n' "$FAKE_WSL_EXECUTABLE"
`
	if err := os.WriteFile(filepath.Join(dir, "wslpath"), []byte(wslpathScript), 0755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", dir)
	t.Setenv("FAKE_WSL_EXECUTABLE", installed)
	forceWSL(t)

	l := NewLauncher("", nil, "", nil)
	p, found := l.detectPlayer()
	if !found {
		t.Fatal("VLC App Paths entry was not detected under WSL")
	}
	if p.Definition.Binary != "vlc.exe" {
		t.Fatalf("detected %q, want vlc.exe", p.Definition.Binary)
	}
	if p.Executable != installed {
		t.Fatalf("resolved executable = %q, want %q", p.Executable, installed)
	}
}

func TestParseRegistryStringQuotedExecutable(t *testing.T) {
	output := []byte("    (Default)    REG_SZ    \"C:\\Program Files\\DAUM\\PotPlayer\\PotPlayerMini64.exe\" \r\n")
	got, found := parseRegistryString(output)
	if !found {
		t.Fatal("registry value not parsed")
	}
	want := `C:\Program Files\DAUM\PotPlayer\PotPlayerMini64.exe`
	if got != want {
		t.Fatalf("registry value = %q, want %q", got, want)
	}
}

func TestPlayerArgs(t *testing.T) {
	t.Run("PotPlayer URL precedes seek switch", func(t *testing.T) {
		got := playerArgs(windowsPlayers[0], "http://media", 42)
		want := []string{"http://media", "/seek=42"}
		if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
			t.Fatalf("args = %#v, want %#v", got, want)
		}
	})

	t.Run("configured PotPlayer keeps all switches after URL", func(t *testing.T) {
		got := configuredPlayerArgs([]string{"/new"}, windowsPlayers[0], true, "http://media", []string{"/seek=42"})
		want := []string{"http://media", "/new", "/seek=42"}
		if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
			t.Fatalf("args = %#v, want %#v", got, want)
		}
	})

	t.Run("VLC uses separate instance for reliable resume", func(t *testing.T) {
		got := playerArgs(PlayerDef{Binary: "vlc", SeekFlag: "--start-time=%d"}, "http://media", 42)
		want := []string{"--no-one-instance", "--start-time=42", "http://media"}
		if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
			t.Fatalf("args = %#v, want %#v", got, want)
		}
	})

	t.Run("VLC play from start can reuse normal instance policy", func(t *testing.T) {
		got := playerArgs(PlayerDef{Binary: "vlc", SeekFlag: "--start-time=%d"}, "http://media", 0)
		want := []string{"http://media"}
		if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
			t.Fatalf("args = %#v, want %#v", got, want)
		}
	})
}

func TestFormatSeekArgs(t *testing.T) {
	tests := []struct {
		name string
		flag string
		want []string
	}{
		{name: "placeholder", flag: "--start=%d", want: []string{"--start=42"}},
		{name: "legacy equals", flag: "--start=", want: []string{"--start=42"}},
		{name: "separate value", flag: "-ss", want: []string{"-ss", "42"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := formatSeekArgs(test.flag, 42)
			if strings.Join(got, "\x00") != strings.Join(test.want, "\x00") {
				t.Fatalf("formatSeekArgs(%q) = %#v, want %#v", test.flag, got, test.want)
			}
		})
	}
}

func TestLookupSeekFlagForAbsoluteWindowsPath(t *testing.T) {
	l := NewLauncher("", nil, "", nil)
	got := l.lookupSeekFlag(`/mnt/c/Program Files/VideoLAN/VLC/vlc.exe`)
	if got != "--start-time=%d" {
		t.Fatalf("seek flag = %q, want --start-time=%%d", got)
	}
}

func TestExpandWindowsEnvironment(t *testing.T) {
	t.Setenv("ProgramFiles", `D:\Applications`)
	got := expandWindowsEnvironment(context.Background(), `%ProgramFiles%\VideoLAN\VLC\vlc.exe`, false)
	want := `D:\Applications\VideoLAN\VLC\vlc.exe`
	if got != want {
		t.Fatalf("expanded path = %q, want %q", got, want)
	}
}

func TestRegistryQueryHonorsContext(t *testing.T) {
	dir := t.TempDir()
	regScript := "#!/bin/sh\nexec /bin/sleep 2\n"
	if err := os.WriteFile(filepath.Join(dir, "reg.exe"), []byte(regScript), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, found := queryRegistryValue(ctx, `HKLM\Software\Example`, "/ve"); found {
		t.Fatal("timed-out registry query unexpectedly returned a value")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("registry query ignored context; elapsed %s", elapsed)
	}
}

// The system-default fallback on WSL uses a Windows opener instead of the
// usually-absent xdg-open, and prefers rundll32 over explorer.exe (which
// mangles URLs containing query strings and opens Documents instead).
func TestLaunchDefaultWSLPrefersRundll32(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only")
	}
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args.txt")
	script := "#!/bin/sh\necho \"$@\" > " + argsFile + "\n"
	if err := os.WriteFile(filepath.Join(dir, "rundll32.exe"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	fakeBinary(t, dir, "explorer.exe")
	t.Setenv("PATH", dir)
	forceWSL(t)

	l := NewLauncher("", nil, "", nil)
	url := "http://server:8096/stream.mkv?Static=true&api_key=x"
	if err := l.launchDefault(url); err != nil {
		t.Fatalf("launchDefault failed: %v", err)
	}

	// Wait for the fake opener to write its argv
	var got []byte
	for i := 0; i < 50; i++ {
		if got, _ = os.ReadFile(argsFile); len(got) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	want := "url.dll,FileProtocolHandler " + url
	if strings.TrimSpace(string(got)) != want {
		t.Fatalf("rundll32 args = %q, want %q", strings.TrimSpace(string(got)), want)
	}
}

// explorer.exe remains the last-resort opener when rundll32 is absent.
func TestLaunchDefaultWSLExplorerLastResort(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only")
	}
	dir := t.TempDir()
	fakeBinary(t, dir, "explorer.exe")
	t.Setenv("PATH", dir)
	forceWSL(t)

	l := NewLauncher("", nil, "", nil)
	if err := l.launchDefault("http://example.invalid/stream"); err != nil {
		t.Fatalf("launchDefault failed: %v", err)
	}
}

// Credential query parameters must never reach the log file.
func TestRedactTokens(t *testing.T) {
	in := []string{
		"--start=30",
		"http://server:8096/Videos/1/stream.mkv?Static=true&api_key=SECRET1",
		"http://server:32400/library/parts/2/file.mkv?X-Plex-Token=SECRET2&other=1",
	}
	out := redactTokens(in)
	joined := strings.Join(out, " ")
	if strings.Contains(joined, "SECRET1") || strings.Contains(joined, "SECRET2") {
		t.Fatalf("token leaked: %q", joined)
	}
	if !strings.Contains(joined, "api_key=REDACTED") || !strings.Contains(joined, "X-Plex-Token=REDACTED") {
		t.Fatalf("redaction markers missing: %q", joined)
	}
	if out[0] != "--start=30" {
		t.Fatalf("non-URL arg mangled: %q", out[0])
	}
	if in[1] == out[1] {
		t.Fatal("input not redacted")
	}
}

// With no player and no opener anywhere, the error must tell the user what
// to do instead of surfacing a raw exec failure.
func TestLaunchDefaultNoOpenerActionableError(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only")
	}
	t.Setenv("PATH", t.TempDir())
	forceWSL(t)

	l := NewLauncher("", nil, "", nil)
	err := l.launchDefault("http://example.invalid/stream")
	if err == nil {
		t.Fatal("expected error with no opener available")
	}
	if !strings.Contains(err.Error(), "player.command") {
		t.Fatalf("error not actionable: %v", err)
	}
}
