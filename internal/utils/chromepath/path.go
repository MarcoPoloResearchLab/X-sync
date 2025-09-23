// Package chromepath provides helpers for discovering Chrome binary locations.
package chromepath

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

const (
	// EnvironmentVariableName is the environment variable consulted when resolving a Chrome binary.
	EnvironmentVariableName = "CHROME_BIN"

	chromeBinaryEmptyPathErrorMessage = "chrome binary path is empty"

	macOSChromeBundlePath = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"

	windowsChromeExecutableName      = "chrome.exe"
	windowsChromeProgramFilesPath    = `C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe`
	windowsChromeProgramFilesX86Path = `C:\\Program Files (x86)\\Google\\Chrome\\Application\\chrome.exe`

	linuxChromeStableExecutableName    = "google-chrome-stable"
	linuxChromeExecutableName          = "google-chrome"
	linuxChromiumExecutableName        = "chromium"
	linuxChromiumBrowserExecutableName = "chromium-browser"
	linuxChromeGenericExecutableName   = "chrome"

	linuxChromeStableExecutablePath    = "/usr/bin/google-chrome-stable"
	linuxChromeExecutablePath          = "/usr/bin/google-chrome"
	linuxChromiumExecutablePath        = "/usr/bin/chromium"
	linuxChromiumBrowserExecutablePath = "/usr/bin/chromium-browser"
	linuxChromiumSnapExecutablePath    = "/snap/bin/chromium"

	linuxChromeFallbackExecutableName = "google-chrome"
)

var (
	linuxExecutableLookupOrder = []string{
		linuxChromeStableExecutableName,
		linuxChromeExecutableName,
		linuxChromiumExecutableName,
		linuxChromiumBrowserExecutableName,
		linuxChromeGenericExecutableName,
	}

	linuxAbsoluteLookupOrder = []string{
		linuxChromeStableExecutablePath,
		linuxChromeExecutablePath,
		linuxChromiumExecutablePath,
		linuxChromiumBrowserExecutablePath,
		linuxChromiumSnapExecutablePath,
	}
)

// Discover returns the most appropriate Chrome binary path for the current platform.
// The CHROME_BIN environment variable takes precedence, followed by platform-specific defaults.
func Discover() string {
	if environmentOverride := strings.TrimSpace(os.Getenv(EnvironmentVariableName)); environmentOverride != "" {
		return environmentOverride
	}
	return DefaultPath()
}

// DefaultPath returns a best-effort Chrome binary path without consulting the environment.
func DefaultPath() string {
	switch runtime.GOOS {
	case "darwin":
		return macOSChromeBundlePath
	case "windows":
		if resolvedPath, lookErr := exec.LookPath(windowsChromeExecutableName); lookErr == nil {
			return resolvedPath
		}
		windowsCandidates := []string{windowsChromeProgramFilesPath, windowsChromeProgramFilesX86Path}
		for _, candidate := range windowsCandidates {
			if _, statErr := os.Stat(candidate); statErr == nil {
				return candidate
			}
		}
		return windowsChromeExecutableName
	default:
		for _, executableName := range linuxExecutableLookupOrder {
			if resolvedPath, lookErr := exec.LookPath(executableName); lookErr == nil {
				return resolvedPath
			}
		}
		for _, candidatePath := range linuxAbsoluteLookupOrder {
			if _, statErr := os.Stat(candidatePath); statErr == nil {
				return candidatePath
			}
		}
		return linuxChromeFallbackExecutableName
	}
}

// ResolveExecutablePath verifies and resolves the provided Chrome binary hint into an executable path.
func ResolveExecutablePath(binaryHint string) (string, error) {
	trimmedHint := strings.TrimSpace(binaryHint)
	if trimmedHint == "" {
		return "", errors.New(chromeBinaryEmptyPathErrorMessage)
	}
	if strings.ContainsRune(trimmedHint, os.PathSeparator) {
		if _, statErr := os.Stat(trimmedHint); statErr != nil {
			return "", statErr
		}
		return trimmedHint, nil
	}
	resolvedPath, lookErr := exec.LookPath(trimmedHint)
	if lookErr != nil {
		return "", lookErr
	}
	return resolvedPath, nil
}
