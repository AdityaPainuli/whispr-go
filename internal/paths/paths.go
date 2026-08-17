package paths

import (
	"os"
	"path/filepath"
	"strings"
)

// Two layouts, detected at runtime:
//
// dev (go run from the repo):
//   models/...                 (gitignored, downloaded by README steps or us)
//   third_party/llama/...      (llama-server + dylibs)
//
// bundled (Whispr.app):
//   Contents/Resources/llama/...                     (llama-server + dylibs)
//   ~/Library/Application Support/whispr-go/models/  (downloaded on first run)
//
// Models live outside the bundle on purpose: they're 1.6GB+, per-user,
// and re-downloadable — an app update shouldn't re-ship or delete them.

func exeDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	exe, _ = filepath.EvalSymlinks(exe)
	return filepath.Dir(exe)
}

// Bundled reports whether we're running from inside a .app.
func Bundled() bool {
	return strings.Contains(exeDir(), ".app/Contents/MacOS")
}

// LlamaServer returns the llama-server binary path. Its dylibs sit next
// to it in both layouts (the binary finds them via @rpath/@loader_path).
func LlamaServer() string {
	if Bundled() {
		return filepath.Join(exeDir(), "..", "Resources", "llama", "llama-server")
	}
	return "third_party/llama/llama-server"
}

// ModelsDir returns where models live, creating it if needed.
func ModelsDir() string {
	if Bundled() {
		base, err := os.UserConfigDir() // ~/Library/Application Support on macOS
		if err != nil {
			base = os.TempDir()
		}
		dir := filepath.Join(base, "whispr-go", "models")
		os.MkdirAll(dir, 0o755)
		return dir
	}
	return "models"
}
