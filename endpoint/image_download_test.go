package endpoint

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// 1x1 PNG.
var onePxPNG = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
	0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4,
	0x89, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x44, 0x41,
	0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00,
	0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE,
	0x42, 0x60, 0x82,
}

func TestDownloadGeneratedImage_FromBase64(t *testing.T) {
	dir := t.TempDir()
	img := GeneratedImage{Bytes: onePxPNG}
	opts := ImageOptions{
		Prompt:    "a red cat",
		OutputDir: dir,
	}
	out, err := downloadGeneratedImage(context.Background(), nil, opts, img, 1, -1)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if out.LocalPath == "" {
		t.Fatal("LocalPath empty")
	}
	if !strings.HasSuffix(out.LocalPath, "a-red-cat-v1.png") {
		t.Errorf("LocalPath = %q; want suffix a-red-cat-v1.png", out.LocalPath)
	}
	got, err := os.ReadFile(out.LocalPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytesEqual(got, onePxPNG) {
		t.Errorf("file bytes do not match PNG payload")
	}
}

func TestDownloadGeneratedImage_FromURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(200)
		_, _ = w.Write(onePxPNG)
	}))
	defer srv.Close()

	dir := t.TempDir()
	img := GeneratedImage{URL: srv.URL + "/img.png"}
	opts := ImageOptions{
		Prompt:    "a blue dog",
		OutputDir: dir,
	}
	out, err := downloadGeneratedImage(context.Background(), srv.Client(), opts, img, 1, -1)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if !strings.HasSuffix(out.LocalPath, "a-blue-dog-v1.png") {
		t.Errorf("LocalPath = %q; want suffix a-blue-dog-v1.png", out.LocalPath)
	}
}

func TestDownloadGeneratedImage_MimeFromContentTypeWhenURLHasNoExt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.WriteHeader(200)
		_, _ = w.Write([]byte{0xFF, 0xD8, 0xFF, 0xE0})
	}))
	defer srv.Close()

	dir := t.TempDir()
	img := GeneratedImage{URL: srv.URL + "/proxy-no-ext"}
	opts := ImageOptions{Prompt: "x", OutputDir: dir}
	out, err := downloadGeneratedImage(context.Background(), srv.Client(), opts, img, 1, -1)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if !strings.HasSuffix(out.LocalPath, ".jpg") {
		t.Errorf("LocalPath = %q; want .jpg extension", out.LocalPath)
	}
}

func TestDownloadGeneratedImage_UnknownExtFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(200)
		_, _ = w.Write([]byte("x"))
	}))
	defer srv.Close()

	dir := t.TempDir()
	img := GeneratedImage{URL: srv.URL + "/blob"}
	opts := ImageOptions{Prompt: "x", OutputDir: dir}
	_, err := downloadGeneratedImage(context.Background(), srv.Client(), opts, img, 1, -1)
	if err == nil {
		t.Fatal("expected error for unknown MIME, got nil")
	}
	if !strings.Contains(err.Error(), "extension") {
		t.Errorf("expected error to mention extension; got %v", err)
	}
}

func TestDownloadGeneratedImage_MissingDirNoCreate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing")
	opts := ImageOptions{Prompt: "x", OutputDir: dir}
	_, err := downloadGeneratedImage(context.Background(), nil, opts, GeneratedImage{Bytes: onePxPNG}, 1, -1)
	if err == nil {
		t.Fatal("expected fs.ErrNotExist")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("got %v; want errors.Is fs.ErrNotExist", err)
	}
}

func TestDownloadGeneratedImage_BatchIndexInFilename(t *testing.T) {
	dir := t.TempDir()
	for idx := 1; idx <= 3; idx++ {
		out, err := downloadGeneratedImage(context.Background(), nil, ImageOptions{
			Prompt:    "many cats",
			OutputDir: dir,
		}, GeneratedImage{Bytes: onePxPNG}, 1, idx)
		if err != nil {
			t.Fatalf("idx=%d: %v", idx, err)
		}
		want := "many-cats-v1-" + strconv.Itoa(idx) + ".png"
		if filepath.Base(out.LocalPath) != want {
			t.Errorf("idx=%d: got %q; want %q", idx, filepath.Base(out.LocalPath), want)
		}
	}
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
