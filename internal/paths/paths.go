package paths

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Two layouts, detected at runtime:
//
// dev (go run from the repo):
//   models/...                 (gitignored, downloaded by README steps or us)
//   third_party/llama/...      (llama-server + dylibs)
//
// distributed:
//   macOS:        Whispr.app/Contents/Resources/llama/...
//   win/linux:    llama/ directory next to the executable
//   models:       user config dir (~/Library/Application Support,
//                 %AppData%, ~/.config) /whispr-go/models, downloaded on
//                 first run
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

func llamaBin() string {
	if runtime.GOOS == "windows" {
		return "llama-server.exe"
	}
	return "llama-server"
}

// distRoot returns the directory holding distributed resources, or ""
// when running from the repo.
func distRoot() string {
	dir := exeDir()
	if runtime.GOOS == "darwin" && strings.Contains(dir, ".app/Contents/MacOS") {
		return filepath.Join(dir, "..", "Resources")
	}
	// win/linux dist archive: llama/ sits next to the executable.
	if _, err := os.Stat(filepath.Join(dir, "llama", llamaBin())); err == nil {
		return dir
	}
	return ""
}

// Distributed reports whether we're running from a dist layout rather
// than the repo.
func Distributed() bool { return distRoot() != "" }

// LlamaServer returns the llama-server binary path. Its dylibs sit next
// to it in both layouts (the binary finds them via @rpath/@loader_path
// on mac, $ORIGIN on linux, same-directory DLL search on windows).
func LlamaServer() string {
	if root := distRoot(); root != "" {
		return filepath.Join(root, "llama", llamaBin())
	}
	return filepath.Join("third_party", "llama", llamaBin())
}

// ModelsDir returns where models live, creating it if needed.
func ModelsDir() string {
	if Distributed() {
		base, err := os.UserConfigDir()
		if err != nil {
			base = os.TempDir()
		}
		dir := filepath.Join(base, "whispr-go", "models")
		os.MkdirAll(dir, 0o755)
		return dir
	}
	return "models"
}
