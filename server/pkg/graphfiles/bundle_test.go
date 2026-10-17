package graphfiles

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestBundleRoundTripInIndependentDirectory(t *testing.T) {
	source := t.TempDir()
	if err := os.MkdirAll(filepath.Join(source, "css"), 0700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"index.html": "<h1>TABELA</h1>", "css/styles.css": "h1{color:green}"} {
		if err := os.WriteFile(filepath.Join(source, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	data, err := Build(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(data); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "review")
	if err := Extract(data, destination); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(destination, "index.html"))
	if err != nil || string(got) != "<h1>TABELA</h1>" {
		t.Fatalf("missing implementation: %s %v", got, err)
	}
	if err := Extract(data, destination); err == nil {
		t.Fatal("existing review directory overwritten")
	}
}

func TestBundleRejectsNonPortablePathsWithValidManifest(t *testing.T) {
	for _, names := range [][]string{{"x?.txt"}, {"x*.txt"}, {"a|b"}, {"a<b"}, {"a\x01b"}, {"a", "A/b.txt"}, {"a/b", "A"}, {"../outside"}, {"C:/outside"}, {"index.html", "INDEX.HTML"}, {".ssh/id_ed25519"}, {".aws/credentials"}, {".npmrc"}, {".netrc"}} {
		t.Run(names[0], func(t *testing.T) {
			var b bytes.Buffer
			w := zip.NewWriter(&b)
			m := manifest{Format: Format}
			for _, name := range names {
				body := []byte("content")
				h := sha256.Sum256(body)
				m.Files = append(m.Files, entry{Path: name, Size: int64(len(body)), SHA256: hex.EncodeToString(h[:])})
				f, err := w.Create(name)
				if err != nil {
					t.Fatal(err)
				}
				_, _ = f.Write(body)
			}
			raw, err := json.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			f, err := w.Create(ManifestName)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = f.Write(raw)
			_ = w.Close()
			if err := Validate(b.Bytes()); err == nil {
				t.Fatalf("nonportable archive accepted: %v", names)
			}
		})
	}
}

func TestBundleRejectsEmptyPrivateAndLinkedFiles(t *testing.T) {
	if _, err := Build(t.TempDir()); err == nil {
		t.Fatal("empty bundle accepted")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("TOKEN=private"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Build(root); err == nil {
		t.Fatal("credential file accepted")
	}
	root = t.TempDir()
	if err := os.Symlink(filepath.Join(t.TempDir(), "outside"), filepath.Join(root, "link")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := Build(root); err == nil {
		t.Fatal("symlink accepted")
	}
}

func TestBundleRejectsUntrustedArchives(t *testing.T) {
	for _, name := range []string{"../outside", "/absolute", "C:/outside", "a\\b", ".git/config", "INDEX.HTML", "index.html"} {
		t.Run(name, func(t *testing.T) {
			var b bytes.Buffer
			w := zip.NewWriter(&b)
			for _, n := range []string{"index.html", name} {
				f, err := w.Create(n)
				if err != nil {
					t.Fatal(err)
				}
				_, _ = f.Write([]byte("body"))
			}
			_ = w.Close()
			if err := Validate(b.Bytes()); err == nil {
				t.Fatal("untrusted archive accepted")
			}
		})
	}
}
