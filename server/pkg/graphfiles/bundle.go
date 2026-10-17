// Package graphfiles defines portable, bounded file deliveries for Graph nodes.
package graphfiles

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	ManifestName     = ".multica-graph-files.json"
	Format           = "multica-graph-files-v1"
	MaxArchiveBytes  = 20 << 20
	MaxExpandedBytes = 32 << 20
	MaxFiles         = 2048
)

type entry struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}
type manifest struct {
	Format string  `json:"format"`
	Files  []entry `json:"files"`
}
type file struct {
	entry
	data []byte
}

func safeName(name string) bool {
	if name == "" || len(name) > 512 || strings.ContainsAny(name, "\\:<>\"|?*\x00") || strings.HasPrefix(name, "/") {
		return false
	}
	for _, ch := range name {
		if ch < 32 || ch == 127 {
			return false
		}
	}
	for _, part := range strings.Split(name, "/") {
		lower := strings.ToLower(part)
		if part == "" || part == "." || part == ".." || strings.TrimRight(part, ". ") != part {
			return false
		}
		if strings.HasPrefix(lower, ".env") || lower == ".git" || lower == ".multica" || lower == ".codex" || lower == ".claude" || lower == ".agent_context" || lower == "node_modules" || lower == ManifestName {
			return false
		}
		if lower == ".ssh" || lower == ".aws" || lower == ".azure" || lower == ".kube" || lower == ".docker" || lower == ".gnupg" || lower == ".npmrc" || lower == ".netrc" || lower == ".pypirc" || lower == ".git-credentials" || lower == "auth.json" || lower == "credentials.json" || lower == "id_rsa" || lower == "id_ed25519" {
			return false
		}
		for _, suffix := range []string{".pem", ".key", ".p12", ".pfx"} {
			if strings.HasSuffix(lower, suffix) {
				return false
			}
		}
		base := strings.SplitN(lower, ".", 2)[0]
		if base == "con" || base == "prn" || base == "aux" || base == "nul" || len(base) == 4 && (strings.HasPrefix(base, "com") || strings.HasPrefix(base, "lpt")) && base[3] >= '1' && base[3] <= '9' {
			return false
		}
	}
	return true
}

// Build packages only the explicitly selected directory. Links, credentials,
// runtime state and dependency trees must not become implicit task outputs.
func Build(root string) ([]byte, error) {
	info, err := os.Lstat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("delivery source must be a directory")
	}
	var files []file
	var total int64
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(rel)
		if !safeName(name) {
			return fmt.Errorf("unsafe or private delivery path %q; select a directory containing only deliverable files", name)
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("delivery cannot contain links: %s", name)
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("delivery requires regular files: %s", name)
		}
		if len(files) >= MaxFiles || info.Size() > MaxExpandedBytes-total {
			return fmt.Errorf("delivery exceeds file or expanded-size limit")
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		opened, err := f.Stat()
		if err != nil {
			_ = f.Close()
			return err
		}
		if !os.SameFile(info, opened) {
			_ = f.Close()
			return fmt.Errorf("delivery source changed while reading: %s", name)
		}
		data, readErr := io.ReadAll(io.LimitReader(f, MaxExpandedBytes-total+1))
		closeErr := f.Close()
		if readErr != nil {
			return readErr
		}
		if closeErr != nil {
			return closeErr
		}
		total += int64(len(data))
		if total > MaxExpandedBytes {
			return fmt.Errorf("delivery exceeds expanded-size limit")
		}
		digest := sha256.Sum256(data)
		files = append(files, file{entry: entry{Path: name, Size: int64(len(data)), SHA256: hex.EncodeToString(digest[:])}, data: data})
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("delivery directory contains no files")
	}
	m := manifest{Format: Format}
	for _, f := range files {
		m.Files = append(m.Files, f.entry)
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	var output bytes.Buffer
	w := zip.NewWriter(&output)
	write := func(name string, data []byte) error {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		header.SetMode(0644)
		f, err := w.CreateHeader(header)
		if err != nil {
			return err
		}
		_, err = f.Write(data)
		return err
	}
	if err = write(ManifestName, raw); err != nil {
		return nil, err
	}
	for _, f := range files {
		if err = write(f.Path, f.data); err != nil {
			return nil, err
		}
	}
	if err = w.Close(); err != nil {
		return nil, err
	}
	if output.Len() > MaxArchiveBytes {
		return nil, fmt.Errorf("delivery archive exceeds %d bytes", MaxArchiveBytes)
	}
	if err = Validate(output.Bytes()); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func read(data []byte) ([]file, error) {
	if len(data) > MaxArchiveBytes {
		return nil, fmt.Errorf("delivery archive exceeds size limit")
	}
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	if len(r.File) < 2 || len(r.File) > MaxFiles+1 {
		return nil, fmt.Errorf("delivery requires 1 to %d files and a manifest", MaxFiles)
	}
	byName := map[string]*zip.File{}
	seen := map[string]bool{}
	var total uint64
	for _, f := range r.File {
		key := strings.ToLower(f.Name)
		if seen[key] || !f.Mode().IsRegular() || f.Name != ManifestName && !safeName(f.Name) {
			return nil, fmt.Errorf("unsafe delivery entry %q", f.Name)
		}
		seen[key] = true
		byName[f.Name] = f
		if f.UncompressedSize64 > MaxExpandedBytes-total {
			return nil, fmt.Errorf("delivery exceeds expanded-size limit")
		}
		total += f.UncompressedSize64
	}
	for name := range byName {
		parts := strings.Split(strings.ToLower(name), "/")
		for i := 1; i < len(parts); i++ {
			if seen[strings.Join(parts[:i], "/")] {
				return nil, fmt.Errorf("delivery file conflicts with a directory: %s", name)
			}
		}
	}
	meta := byName[ManifestName]
	if meta == nil || meta.UncompressedSize64 > 512<<10 {
		return nil, fmt.Errorf("delivery manifest missing or too large")
	}
	load := func(f *zip.File) ([]byte, error) {
		r, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer r.Close()
		return io.ReadAll(io.LimitReader(r, int64(f.UncompressedSize64)+1))
	}
	raw, err := load(meta)
	if err != nil {
		return nil, err
	}
	var m manifest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&m); err != nil {
		return nil, err
	}
	if m.Format != Format || len(m.Files) == 0 || len(m.Files) != len(r.File)-1 {
		return nil, fmt.Errorf("invalid delivery manifest")
	}
	var files []file
	seen = map[string]bool{}
	for _, e := range m.Files {
		f := byName[e.Path]
		key := strings.ToLower(e.Path)
		if !safeName(e.Path) || seen[key] || f == nil || e.Size < 0 || uint64(e.Size) != f.UncompressedSize64 {
			return nil, fmt.Errorf("delivery manifest entry mismatch: %s", e.Path)
		}
		seen[key] = true
		body, err := load(f)
		if err != nil {
			return nil, err
		}
		digest := sha256.Sum256(body)
		if int64(len(body)) != e.Size || hex.EncodeToString(digest[:]) != e.SHA256 {
			return nil, fmt.Errorf("delivery file integrity mismatch: %s", e.Path)
		}
		files = append(files, file{entry: e, data: body})
	}
	return files, nil
}

// Validate verifies every byte and path before a server records a bundle receipt.
func Validate(data []byte) error { _, err := read(data); return err }

// Extract writes only validated files beneath a newly created destination.
// It never merges into or overwrites a previous attempt's output directory.
func Extract(data []byte, destination string) error {
	files, err := read(data)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
		return err
	}
	if err = os.Mkdir(destination, 0700); err != nil {
		return fmt.Errorf("create fresh delivery directory: %w", err)
	}
	for _, f := range files {
		path := filepath.Join(destination, filepath.FromSlash(f.Path))
		if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
		if err = os.WriteFile(path, f.data, 0600); err != nil {
			return err
		}
	}
	return nil
}
