package auth

import (
	"log/slog"
	"os"
	"strings"
	"sync"
)

// Dev mode constants
const (
	// DevToken is the special token accepted when MULTICA_DEV_MODE=true
	DevToken = "dev_local_test_token_12345"

	// DevUserID is the user ID returned when dev token is validated
	DevUserID = "00000000-0000-0000-0000-000000000001"

	// DevWorkspaceIDValue is the workspace ID used in dev mode
	DevWorkspaceIDValue = "00000000-0000-0000-0000-000000000001"

	// DevDaemonIDValue is the daemon ID used in dev mode
	DevDaemonIDValue = "019fc363-9a09-7ca3-85c8-0f3635b0ccf9"
)

var (
	devModeEnabled     bool
	devModeEnabledOnce sync.Once
)

// isDevMode checks if MULTICA_DEV_MODE environment variable is set to "true"
func isDevMode() bool {
	devModeEnabledOnce.Do(func() {
		devModeStr := strings.ToLower(strings.TrimSpace(os.Getenv("MULTICA_DEV_MODE")))
		devModeEnabled = devModeStr == "true" || devModeStr == "1"
		if devModeEnabled {
			slog.Warn("DEV MODE ENABLED - authentication bypass active. DO NOT use in production!")
		}
	})
	return devModeEnabled
}

// ValidateDevToken checks if the given token is the dev token and dev mode is enabled.
// Returns the dev user ID if valid, empty string otherwise.
func ValidateDevToken(token string) string {
	if !isDevMode() {
		return ""
	}

	if token == DevToken {
		return DevUserID
	}

	return ""
}

// DevWorkspaceID returns the dev workspace ID if dev mode is enabled, empty string otherwise.
func DevWorkspaceID() string {
	if !isDevMode() {
		return ""
	}
	return DevWorkspaceIDValue
}

// DevDaemonID returns the dev daemon ID if dev mode is enabled, empty string otherwise.
func DevDaemonID() string {
	if !isDevMode() {
		return ""
	}
	return DevDaemonIDValue
}
