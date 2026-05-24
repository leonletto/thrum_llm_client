package endpoint

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSlugify(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"basic", "a red cat", "a-red-cat"},
		{"unicode-stripped", "café au lait", "caf-au-lait"},
		{"runs collapsed", "  many   spaces  ", "many-spaces"},
		{"leading/trailing dashes trimmed", "---foo---", "foo"},
		{"punctuation", "What's up?!", "what-s-up"},
		{"empty input", "", "image"},
		{"only punctuation", "!!!", "image"},
		{"long input capped at 50 chars", "the quick brown fox jumps over the lazy dog and then some more text", "the-quick-brown-fox-jumps-over-the-lazy-dog-and-th"},
		{"mixed case", "Hello WORLD", "hello-world"},
		{"path traversal attempt", "../../etc/passwd", "etc-passwd"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := slugify(c.in)
			if got != c.want {
				t.Errorf("slugify(%q) = %q; want %q", c.in, got, c.want)
			}
		})
	}
}

func TestMimeToExt(t *testing.T) {
	cases := []struct {
		mime string
		want string
		ok   bool
	}{
		{"image/png", "png", true},
		{"image/jpeg", "jpg", true},
		{"image/jpg", "jpg", true},
		{"image/webp", "webp", true},
		{"image/gif", "gif", true},
		{"video/mp4", "mp4", true},
		{"video/webm", "webm", true},
		{"image/png; charset=binary", "png", true},
		{"application/octet-stream", "", false},
		{"", "", false},
		{"text/plain", "", false},
	}
	for _, c := range cases {
		t.Run(c.mime, func(t *testing.T) {
			got, ok := mimeToExt(c.mime)
			if got != c.want || ok != c.ok {
				t.Errorf("mimeToExt(%q) = (%q,%v); want (%q,%v)", c.mime, got, ok, c.want, c.ok)
			}
		})
	}
}

func TestExtFromURL(t *testing.T) {
	cases := []struct {
		url  string
		want string
	}{
		{"https://cdn.example.com/foo/bar.png", "png"},
		{"https://cdn.example.com/foo/bar.JPG", "jpg"},
		{"https://cdn.example.com/foo/bar.jpeg?signed=1&exp=99", "jpg"},
		{"https://cdn.example.com/foo/bar.mp4", "mp4"},
		{"https://cdn.example.com/foo/bar", ""},
		{"", ""},
	}
	for _, c := range cases {
		t.Run(c.url, func(t *testing.T) {
			if got := extFromURL(c.url); got != c.want {
				t.Errorf("extFromURL(%q) = %q; want %q", c.url, got, c.want)
			}
		})
	}
}

func TestPickVersion_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	n, err := pickVersion(dir, "a-red-cat", "png")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if n != 1 {
		t.Errorf("got v=%d; want 1", n)
	}
}

func TestPickVersion_NextAfterExisting(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		"a-red-cat-v1.png",
		"a-red-cat-v2.png",
		"a-red-cat-v5.png",
		"other-prompt-v1.png",
		"a-red-cat-v3-1.png",
		"a-red-cat-v3-2.png",
	} {
		if err := writeBlank(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	n, err := pickVersion(dir, "a-red-cat", "png")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if n != 6 {
		t.Errorf("got v=%d; want 6", n)
	}
}

// Locks in cross-form behavior: a directory containing ONLY batch
// files must still count their N values when a subsequent single
// pickVersion call runs. Without this, the function would silently
// return 1, producing v1.png next to v5-1.png.
func TestPickVersion_OnlyBatchFilesPresent(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		"a-red-cat-v3-1.png",
		"a-red-cat-v5-1.png",
	} {
		if err := writeBlank(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	n, err := pickVersion(dir, "a-red-cat", "png")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if n != 6 {
		t.Errorf("got v=%d; want 6", n)
	}
}

func TestBuildFilename(t *testing.T) {
	cases := []struct {
		name string
		slug string
		v    int
		idx  int
		ext  string
		want string
	}{
		{"single", "a-red-cat", 1, -1, "png", "a-red-cat-v1.png"},
		{"batch member", "a-red-cat", 2, 1, "png", "a-red-cat-v2-1.png"},
		{"batch member higher idx", "a-red-cat", 7, 4, "jpg", "a-red-cat-v7-4.jpg"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := buildFilename(c.slug, c.v, c.idx, c.ext)
			if got != c.want {
				t.Errorf("buildFilename(%q,%d,%d,%q) = %q; want %q", c.slug, c.v, c.idx, c.ext, got, c.want)
			}
		})
	}
}

func writeBlank(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	return f.Close()
}
