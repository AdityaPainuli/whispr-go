// Package models downloads the ASR and cleanup models on first run, so
// nobody has to hand-run curl commands from a README. Everything is
// idempotent: present files are never re-downloaded, interrupted downloads
// land in a .partial file and restart cleanly next launch.
package models

import (
	"archive/tar"
	"compress/bzip2"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const (
	asrURL = "https://github.com/k2-fsa/sherpa-onnx/releases/download/asr-models/sherpa-onnx-nemotron-speech-streaming-en-0.6b-560ms-int8-2026-04-25.tar.bz2"
	// The tarball's top-level directory, stripped to models/nemotron-en.
	asrTarDir = "sherpa-onnx-nemotron-speech-streaming-en-0.6b-560ms-int8-2026-04-25"

	qwen15URL = "https://huggingface.co/Qwen/Qwen2.5-1.5B-Instruct-GGUF/resolve/main/qwen2.5-1.5b-instruct-q4_k_m.gguf?download=true"
	qwen3URL  = "https://huggingface.co/Qwen/Qwen2.5-3B-Instruct-GGUF/resolve/main/qwen2.5-3b-instruct-q4_k_m.gguf?download=true"

	// sha256 of the ggufs, matching HuggingFace's published LFS hashes.
	// A truncated or tampered download must never be loaded as a model.
	qwen15SHA = "6a1a2eb6d15622bf3c96857206351ba97e1af16c30d7a74ee38970e434e9407e"
	qwen3SHA  = "626b4a6678b86442240e33df819e00132d3ba7dddfe1cdc4fbb18e0a9615c62d"
)

// ASRPresent reports whether the ASR model files exist under dir.
func ASRPresent(dir string) bool {
	for _, f := range []string{"encoder.int8.onnx", "decoder.int8.onnx", "joiner.int8.onnx", "tokens.txt"} {
		if _, err := os.Stat(filepath.Join(dir, "nemotron-en", f)); err != nil {
			return false
		}
	}
	return true
}

// CleanupModel returns the gguf filename for this machine's tier.
func CleanupModel(corrections bool) string {
	if corrections {
		return "qwen2.5-3b-instruct-q4_k_m.gguf"
	}
	return "qwen2.5-1.5b-instruct-q4_k_m.gguf"
}

// Ensure downloads whatever is missing from dir. progress receives short
// human-readable status lines ("ASR model 42%") — safe to show in a menu
// bar title. Blocking; call from a goroutine. cleanup=false skips the
// LLM download entirely (refine disabled in config: raw ASR only).
func Ensure(dir string, cleanup, corrections bool, progress func(string)) error {
	if !ASRPresent(dir) {
		if err := fetchASR(dir, progress); err != nil {
			return fmt.Errorf("models: ASR download: %w", err)
		}
	}
	if !cleanup {
		return nil
	}
	gguf := CleanupModel(corrections)
	if _, err := os.Stat(filepath.Join(dir, gguf)); err != nil {
		url, sha := qwen15URL, qwen15SHA
		if corrections {
			url, sha = qwen3URL, qwen3SHA
		}
		if err := fetchFile(url, sha, filepath.Join(dir, gguf), "cleanup model", progress); err != nil {
			return fmt.Errorf("models: cleanup download: %w", err)
		}
	}
	return nil
}

// fetchASR streams the tar.bz2 straight to disk — decompress and untar in
// flight, no temp archive. The top-level dir is renamed to nemotron-en
// only after the whole tar extracts, so a torn download can't look done.
func fetchASR(dir string, progress func(string)) error {
	tmpRoot := filepath.Join(dir, ".nemotron-en.partial")
	os.RemoveAll(tmpRoot)
	defer os.RemoveAll(tmpRoot)

	resp, err := http.Get(asrURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("status %d", resp.StatusCode)
	}

	counted := &countingReader{r: resp.Body, total: resp.ContentLength,
		report: func(pct int) { progress(fmt.Sprintf("ASR model %d%%", pct)) }}
	tr := tar.NewReader(bzip2.NewReader(counted))
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(hdr.Name, asrTarDir+"/")
		// Path traversal guard: the archive is fetched over the network.
		if rel == "" || strings.Contains(rel, "..") {
			continue
		}
		dest := filepath.Join(tmpRoot, rel)
		switch hdr.Typeflag {
		case tar.TypeDir:
			os.MkdirAll(dest, 0o755)
		case tar.TypeReg:
			os.MkdirAll(filepath.Dir(dest), 0o755)
			f, err := os.Create(dest)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			f.Close()
		}
	}
	return os.Rename(tmpRoot, filepath.Join(dir, "nemotron-en"))
}

// fetchFile downloads url to dest via a .partial rename, hashing in
// flight — an interrupted, truncated, or tampered download never
// masquerades as a complete model.
func fetchFile(url, wantSHA, dest, label string, progress func(string)) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("status %d", resp.StatusCode)
	}

	tmp := dest + ".partial"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	counted := &countingReader{r: resp.Body, total: resp.ContentLength,
		report: func(pct int) { progress(fmt.Sprintf("%s %d%%", label, pct)) }}
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, h), counted); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	f.Close()
	if got := hex.EncodeToString(h.Sum(nil)); got != wantSHA {
		os.Remove(tmp)
		return fmt.Errorf("%s checksum mismatch: got %s want %s", label, got, wantSHA)
	}
	return os.Rename(tmp, dest)
}

// countingReader reports whole-percent progress as it's read through.
type countingReader struct {
	r      io.Reader
	total  int64
	read   int64
	last   int
	report func(pct int)
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.read += int64(n)
	if c.total > 0 {
		if pct := int(c.read * 100 / c.total); pct != c.last {
			c.last = pct
			c.report(pct)
		}
	}
	return n, err
}
