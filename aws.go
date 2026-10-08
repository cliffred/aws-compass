package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Credentials as printed by `aws configure export-credentials --format process`.
type Credentials struct {
	AccessKeyID     string `json:"AccessKeyId"`
	SecretAccessKey string `json:"SecretAccessKey"`
	SessionToken    string `json:"SessionToken"` // empty for long-term keys
	Expiration      string `json:"Expiration"`   // RFC 3339, empty for long-term keys
}

// awsCLI is the command used for AWS calls; tests point it at a fake.
var awsCLI = "aws"

// getCredentials returns temporary credentials for profile. If the cached
// session can't produce them (or forceLogin is set), it runs
// `aws sso login` and tries again.
func getCredentials(profile string, forceLogin bool, status io.Writer) (*Credentials, error) {
	if _, err := exec.LookPath(awsCLI); err != nil {
		return nil, fmt.Errorf("the AWS CLI (%s) was not found in PATH", awsCLI)
	}

	if !forceLogin {
		creds, _, err := exportCredentials(profile)
		if err == nil {
			return creds, nil
		}
	}

	fmt.Fprintf(status, "Logging in to AWS SSO for profile %s...\n", profile)
	if err := ssoLogin(profile); err != nil {
		return nil, err
	}

	creds, stderr, err := exportCredentials(profile)
	if err != nil {
		msg := strings.TrimSpace(stderr)
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("getting credentials for profile %s: %s", profile, msg)
	}
	return creds, nil
}

func exportCredentials(profile string) (*Credentials, string, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(awsCLI, "configure", "export-credentials", "--profile", profile, "--format", "process")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, stderr.String(), err
	}

	var creds Credentials
	if err := json.Unmarshal(stdout.Bytes(), &creds); err != nil {
		return nil, "", fmt.Errorf("unexpected output from aws configure export-credentials: %w", err)
	}
	if creds.AccessKeyID == "" || creds.SecretAccessKey == "" {
		return nil, "", fmt.Errorf("aws configure export-credentials returned no access key")
	}
	return &creds, "", nil
}

func ssoLogin(profile string) error {
	cmd := exec.Command(awsCLI, "sso", "login", "--profile", profile)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stderr // keep stdout clean for --print
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("aws sso login --profile %s failed: %w", profile, err)
	}
	return nil
}

// describeExpiry returns e.g. "valid until 17:42 (2h59m)", or "" if the
// credentials have no expiry (long-term keys).
func describeExpiry(expiration string, now time.Time) string {
	if expiration == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, expiration)
	if err != nil {
		return ""
	}
	left := t.Sub(now).Round(time.Minute)
	if left <= 0 {
		return "expired at " + t.Local().Format("15:04")
	}
	return fmt.Sprintf("valid until %s (%s)", t.Local().Format("15:04"), strings.TrimSuffix(left.String(), "0s"))
}
