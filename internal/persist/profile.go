package persist

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ValidateName reports whether name is usable as a single path element. A
// profile name reaches the filesystem, so it must not be able to escape the
// profiles directory or smuggle a separator in.
func ValidateName(name string) error {
	switch {
	case name == "":
		return errors.New("empty profile name")
	case len(name) > 64:
		return fmt.Errorf("profile name is %d bytes, the limit is 64", len(name))
	case name == "." || name == "..":
		return errors.New("profile name must not be a path element")
	case strings.ContainsAny(name, `/\`), strings.ContainsRune(name, 0):
		return errors.New("profile name must not contain a path separator")
	case filepath.Base(name) != name:
		return errors.New("profile name must be a single path element")
	}
	return nil
}

// ProfileDir returns <data dir>/profiles/<name>, creating it 0700.
func ProfileDir(dir, name string) (string, error) {
	if err := ValidateName(name); err != nil {
		return "", fmt.Errorf("profile %q: %w", name, err)
	}
	if strings.TrimSpace(dir) == "" {
		return "", fmt.Errorf("profile %q: empty data directory", name)
	}
	p := filepath.Join(dir, ProfilesSubdir, name)
	if err := os.MkdirAll(p, 0o700); err != nil {
		return "", fmt.Errorf("profile %q: %w", name, err)
	}
	// MkdirAll cannot add bits the umask removed, but an existing directory may
	// have been created by hand with looser permissions.
	if err := os.Chmod(p, 0o700); err != nil {
		return "", fmt.Errorf("profile %q: mode: %w", name, err)
	}
	return p, nil
}

// ProfileEnv returns the environment that points one page host at dir. Where
// the engine keeps website data is a per-platform decision, and each of the
// three has exactly one lever:
//
//	linux   WebKitGTK follows the XDG base data directory ($XDG_DATA_HOME/<program>)
//	windows WebView2 keeps its user data under %APPDATA%\<executable>
//	darwin  WKWebView exposes no directory API; HOME is the only lever, and it
//	        moves everything the process reads from the home directory
//
// goos is a parameter rather than runtime.GOOS so every branch is testable on
// one machine.
func ProfileEnv(goos, dir string) []string {
	switch goos {
	case "linux":
		return []string{
			"XDG_DATA_HOME=" + filepath.Join(dir, "data"),
			"XDG_CACHE_HOME=" + filepath.Join(dir, "cache"),
		}
	case "windows":
		return []string{"APPDATA=" + filepath.Join(dir, "appdata")}
	case "darwin":
		return []string{"HOME=" + filepath.Join(dir, "home")}
	default:
		return nil
	}
}
