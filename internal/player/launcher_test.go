package player

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func fakeBinary(t *testing.T, dir, name string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
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
	if p.Binary != "mpv.exe" {
		t.Fatalf("detected %q, want mpv.exe", p.Binary)
	}
	if p.SeekFlag != "--start=%d" {
		t.Fatalf("wrong seek flag %q", p.SeekFlag)
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
	if !found || p.Binary != "PotPlayerMini64.exe" {
		t.Fatalf("expected PotPlayer to win, got %q (found=%v)", p.Binary, found)
	}
	if p.SeekFlag != "/seek=%d" {
		t.Fatalf("wrong PotPlayer seek flag %q", p.SeekFlag)
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
	if !found || p.Binary != "mpv" {
		t.Fatalf("expected native mpv to win, got %q (found=%v)", p.Binary, found)
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
