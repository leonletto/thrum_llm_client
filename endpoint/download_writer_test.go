package endpoint

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureOutputDir_Exists(t *testing.T) {
	dir := t.TempDir()
	if err := ensureOutputDir(dir, false); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}

func TestEnsureOutputDir_MissingNoCreate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "does-not-exist")
	err := ensureOutputDir(dir, false)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("want errors.Is(err, fs.ErrNotExist); got %v", err)
	}
	if !strings.Contains(err.Error(), "CreateOutputDir") {
		t.Errorf("expected error text to include tip about CreateOutputDir; got %q", err.Error())
	}
}

func TestEnsureOutputDir_MissingCreate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "deeply", "made")
	if err := ensureOutputDir(dir, true); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	st, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("expected dir to exist: %v", err)
	}
	if !st.IsDir() {
		t.Errorf("expected dir, got file")
	}
}

func TestEnsureOutputDir_PathIsFile(t *testing.T) {
	tmp := t.TempDir()
	bogus := filepath.Join(tmp, "i-am-a-file")
	if err := os.WriteFile(bogus, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := ensureOutputDir(bogus, false)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("expected error to mention 'not a directory'; got %q", err.Error())
	}
}

func TestWriteFileExcl_FreshFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a-red-cat-v1.png")
	src := bytes.NewReader([]byte("PNG-bytes"))
	n, err := writeFileExcl(path, src, nil)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if n != int64(len("PNG-bytes")) {
		t.Errorf("wrote %d; want 9", n)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "PNG-bytes" {
		t.Errorf("file content = %q; want PNG-bytes", string(got))
	}
}

func TestWriteFileExcl_ExistingFileReturnsEEXIST(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "exists.png")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := writeFileExcl(path, bytes.NewReader([]byte("new")), nil)
	if err == nil {
		t.Fatal("expected EEXIST error, got nil")
	}
	if !errors.Is(err, fs.ErrExist) {
		t.Errorf("want errors.Is(err, fs.ErrExist); got %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "old" {
		t.Errorf("existing file mutated; content = %q; want old", string(got))
	}
}

func TestWriteFileExcl_UnlinksOnCopyFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fail.png")
	src := &failingReader{good: []byte("partial-"), failAfter: 8}
	_, err := writeFileExcl(path, src, nil)
	if err == nil {
		t.Fatal("expected copy error, got nil")
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, fs.ErrNotExist) {
		t.Errorf("expected partial file to be unlinked; stat err = %v", statErr)
	}
}

type failingReader struct {
	good      []byte
	failAfter int
	read      int
}

func (r *failingReader) Read(p []byte) (int, error) {
	if r.read >= r.failAfter {
		return 0, errors.New("network error")
	}
	n := copy(p, r.good[r.read:])
	r.read += n
	return n, nil
}

var _ io.Reader = (*failingReader)(nil)
