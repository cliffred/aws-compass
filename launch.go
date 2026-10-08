package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/atotto/clipboard"
)

func compassPath(cfg *Config) (string, error) {
	if cfg.Defaults.CompassPath != "" {
		return expandHome(cfg.Defaults.CompassPath)
	}
	switch runtime.GOOS {
	case "darwin":
		return "/Applications/MongoDB Compass.app/Contents/MacOS/MongoDB Compass", nil
	case "windows":
		return filepath.Join(os.Getenv("LOCALAPPDATA"), "MongoDBCompass", "MongoDBCompass.exe"), nil
	default:
		return "mongodb-compass", nil
	}
}

// launchCompass starts Compass in its own session, so it keeps running
// after aws-compass exits and isn't killed when the terminal closes.
func launchCompass(path, uri string) error {
	exe, err := exec.LookPath(path)
	if err != nil {
		return fmt.Errorf("MongoDB Compass not found at %q; set defaults.compassPath in the config", path)
	}
	cmd := exec.Command(exe, uri) // stdio left nil: Compass's log noise goes to /dev/null
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting MongoDB Compass: %w", err)
	}
	return cmd.Process.Release()
}

// runMongosh runs mongosh in the foreground. It passes the profile instead of
// the credentials: mongosh's AWS SDK reads the SSO session itself, refreshes
// it when it expires, and the secrets stay out of the process list.
func runMongosh(path, uri, profile string) error {
	if path == "" {
		path = "mongosh"
	}
	exe, err := exec.LookPath(path)
	if err != nil {
		return fmt.Errorf("mongosh not found at %q; install it or set defaults.mongoshPath in the config", path)
	}

	cmd := exec.Command(exe, uri)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = append(withoutAWSCredentials(os.Environ()), "AWS_PROFILE="+profile)

	// Ctrl-C belongs to mongosh, not to us.
	signal.Ignore(os.Interrupt)
	defer signal.Reset(os.Interrupt)

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitCode(exitErr.ExitCode())
		}
		return fmt.Errorf("running mongosh: %w", err)
	}
	return nil
}

// withoutAWSCredentials drops credential variables that would take priority
// over AWS_PROFILE.
func withoutAWSCredentials(env []string) []string {
	out := env[:0:0]
	for _, kv := range env {
		key, _, _ := strings.Cut(kv, "=")
		switch key {
		case "AWS_PROFILE", "AWS_DEFAULT_PROFILE", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN":
			continue
		}
		out = append(out, kv)
	}
	return out
}

func copyToClipboard(s string) error {
	if err := clipboard.WriteAll(s); err != nil {
		return fmt.Errorf("copying to the clipboard: %w", err)
	}
	return nil
}
