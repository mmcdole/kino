package adapter

import (
	"fmt"
	"log/slog"
	"os/exec"
	"runtime"
	"time"
)

// Launcher launches media URLs in an external player
type Launcher struct {
	command string   // configured player command, empty for system default
	args    []string // additional arguments for the player
	logger  *slog.Logger
}

// NewLauncher creates a new Launcher
func NewLauncher(command string, args []string, logger *slog.Logger) *Launcher {
	if logger == nil {
		logger = slog.Default()
	}
	return &Launcher{
		command: command,
		args:    args,
		logger:  logger,
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
	if startOffset > 0 {
		// mpv-style start offset argument
		args = append(args, fmt.Sprintf("--start=%f", startOffset.Seconds()))
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
