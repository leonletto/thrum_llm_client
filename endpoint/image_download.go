package endpoint

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"path/filepath"
)

// downloadGeneratedImage writes img to opts.OutputDir, returning a
// copy of img with LocalPath populated. When img.Bytes is non-empty
// the bytes are written directly; otherwise the URL is HTTP-GETed
// via client and streamed to disk.
//
// version is the N in {slug}-vN.{ext} / {slug}-vN-{idx}.{ext}. The
// orchestrator (UnifiedImageClient.GenerateImage or DownloadVideo)
// computes version ONCE per batch via pickVersion so all members
// of the batch share a single N (batch-atomic versioning). On
// rare EEXIST under concurrent writers, this helper bumps locally
// and retries.
//
// batchIdx is -1 for non-batch (N=1) results and >= 1 for the
// idx-th member of a batch.
//
// client may be nil when opts.OutputDir is being satisfied entirely
// from base64 (Bytes), in which case no HTTP is performed.
func downloadGeneratedImage(ctx context.Context, client *http.Client, opts ImageOptions, img GeneratedImage, version, batchIdx int) (GeneratedImage, error) {
	if err := ensureOutputDir(opts.OutputDir, opts.CreateOutputDir); err != nil {
		return img, err
	}
	slug := slugify(opts.Prompt)

	var (
		src      io.Reader
		ext      string
		totalLen int64 = -1
		closer   io.Closer
	)
	switch {
	case len(img.Bytes) > 0:
		src = bytes.NewReader(img.Bytes)
		totalLen = int64(len(img.Bytes))
		ext = "png"

	case img.URL != "":
		if client == nil {
			client = http.DefaultClient
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, img.URL, nil)
		if err != nil {
			return img, fmt.Errorf("image download: build request: %w", err)
		}
		resp, err := client.Do(req)
		if err != nil {
			return img, fmt.Errorf("image download: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			_ = resp.Body.Close()
			// CDN/storage download is not a provider API call —
			// plain fmt.Errorf, no sentinel wrap.
			return img, fmt.Errorf("image download: status %d: %s", resp.StatusCode, string(body))
		}
		src = resp.Body
		closer = resp.Body
		if resp.ContentLength > 0 {
			totalLen = resp.ContentLength
		}
		if e := extFromURL(img.URL); e != "" {
			ext = e
		} else if ct := resp.Header.Get("Content-Type"); ct != "" {
			if e, ok := mimeToExt(ct); ok {
				ext = e
			}
		}
		if ext == "" {
			_ = resp.Body.Close()
			return img, fmt.Errorf("image download: could not determine file extension from URL %q or Content-Type %q", img.URL, resp.Header.Get("Content-Type"))
		}

	default:
		return img, fmt.Errorf("image download: GeneratedImage has neither URL nor Bytes populated")
	}
	if closer != nil {
		defer closer.Close()
	}

	const maxRetries = 10
	v := version
	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		name := buildFilename(slug, v, batchIdx, ext)
		path := filepath.Join(opts.OutputDir, name)

		var err error
		if opts.OnProgress != nil {
			pr, pw := io.Pipe()
			errc := make(chan error, 1)
			go func() {
				_, copyErr := copyWithProgress(pw, src, totalLen, opts.OnProgress)
				_ = pw.CloseWithError(copyErr)
				errc <- copyErr
			}()
			_, err = writeFileExcl(path, pr, nil)
			// Unblock the producer goroutine if writeFileExcl
			// errored mid-copy: nobody is draining the pipe, so
			// pw.Write would block forever. CloseWithError makes
			// the goroutine's next Read return our err, the
			// goroutine then exits and sends on errc.
			_ = pr.CloseWithError(err)
			if cerr := <-errc; err == nil && cerr != nil {
				err = cerr
			}
		} else {
			_, err = writeFileExcl(path, src, nil)
		}

		if err == nil {
			out := img
			out.LocalPath = path
			return out, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return img, err
		}
		lastErr = err
		// Rare race: another writer occupied this exact filename
		// between pickVersion and our O_EXCL. Re-scan to find the
		// next free version and try again (only meaningful for
		// the in-memory bytes path; streaming sources cannot be
		// replayed).
		nv, perr := pickVersion(opts.OutputDir, slug, ext)
		if perr != nil {
			return img, fmt.Errorf("image download: pick version on EEXIST retry: %w", perr)
		}
		v = nv
		if br, ok := src.(*bytes.Reader); ok {
			_, _ = br.Seek(0, io.SeekStart)
			continue
		}
		return img, fmt.Errorf("image download: filename collision and source is non-seekable: %w", lastErr)
	}
	return img, fmt.Errorf("image download: exceeded %d retries on filename collision: %w", maxRetries, lastErr)
}
