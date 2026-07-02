package player

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
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

// The system-default fallback on WSL uses wslview or explorer.exe instead of
// the usually-absent xdg-open.
func TestLaunchDefaultWSLUsesWindowsOpener(t *testing.T) {
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
