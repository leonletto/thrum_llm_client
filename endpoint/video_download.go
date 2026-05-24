package endpoint

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
)

// DownloadVideo writes each completed video in job to opts.OutputDir.
// On success the matching GeneratedVideo.LocalPath fields are
// populated in place. The function is a no-op when job.Status is
// not JobStatusCompleted (callers handle non-terminal states
// themselves; partial-failure semantics belong to the job, not the
// downloader).
//
// Multiple videos are downloaded sequentially; on the first error
// the function returns without touching subsequent entries. Already-
// written LocalPath fields remain populated so callers can recover.
func DownloadVideo(ctx context.Context, job *VideoJob, opts VideoOptions) error {
	if job == nil || job.Status != JobStatusCompleted || len(job.Videos) == 0 {
		return nil
	}
	if opts.OutputDir == "" {
		return nil
	}
	if err := ensureOutputDir(opts.OutputDir, opts.CreateOutputDir); err != nil {
		return err
	}
	slug := slugify(opts.Prompt)

	// Batch-atomic versioning: pick N ONCE for the whole job. Today
	// every provider returns exactly one video per job, but the
	// orchestration is identical to the image side — if a future
	// provider returns multiple per job they share a single N.
	// Caveat: extension is per-video (URL-derived). We use the
	// first video's extension to scan; revisit if any provider
	// mixes container types within one job.
	firstExt := extFromURL(job.Videos[0].URL)
	if firstExt == "" {
		firstExt = "mp4"
	}
	nv, perr := pickVersion(opts.OutputDir, slug, firstExt)
	if perr != nil {
		return fmt.Errorf("pick version: %w", perr)
	}

	for i := range job.Videos {
		v := &job.Videos[i]
		if v.OpenContent == nil {
			return fmt.Errorf("video[%d]: OpenContent is nil (job not in completed state?)", i)
		}
		ext := extFromURL(v.URL)
		if ext == "" {
			ext = "mp4"
		}

		rc, err := v.OpenContent(ctx)
		if err != nil {
			return fmt.Errorf("video[%d] open content: %w", i, err)
		}
		name := buildFilename(slug, nv, -1, ext)
		path := filepath.Join(opts.OutputDir, name)

		var copyErr error
		if opts.OnProgress != nil {
			pr, pw := io.Pipe()
			errc := make(chan error, 1)
			go func() {
				_, cerr := copyWithProgress(pw, rc, -1, opts.OnProgress)
				_ = pw.CloseWithError(cerr)
				errc <- cerr
			}()
			_, copyErr = writeFileExcl(path, pr, nil)
			// Unblock the producer goroutine on writer-side
			// failure (see image_download.go for rationale).
			_ = pr.CloseWithError(copyErr)
			if cerr := <-errc; copyErr == nil && cerr != nil {
				copyErr = cerr
			}
		} else {
			_, copyErr = writeFileExcl(path, rc, nil)
		}
		_ = rc.Close()

		if copyErr != nil {
			if errors.Is(copyErr, fs.ErrExist) {
				return fmt.Errorf("video[%d] filename collision on %q (parallel writer?): %w", i, path, copyErr)
			}
			return fmt.Errorf("video[%d] write: %w", i, copyErr)
		}
		v.LocalPath = path
	}
	return nil
}
