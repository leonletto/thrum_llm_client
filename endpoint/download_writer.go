package endpoint

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
)

// ensureOutputDir verifies that dir exists and is a directory.
// When createIfMissing is true and dir is missing, it is created
// with os.MkdirAll (perm 0o755). When createIfMissing is false
// and dir is missing, returns an error wrapping fs.ErrNotExist
// with a tip pointing callers at the CreateOutputDir option.
func ensureOutputDir(dir string, createIfMissing bool) error {
	st, err := os.Stat(dir)
	switch {
	case err == nil:
		if !st.IsDir() {
			return fmt.Errorf("output path %q is not a directory", dir)
		}
		return nil
	case errors.Is(err, fs.ErrNotExist):
		if !createIfMissing {
			return fmt.Errorf("output directory %q does not exist (set CreateOutputDir=true to auto-create): %w", dir, fs.ErrNotExist)
		}
		if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
			return fmt.Errorf("output directory %q: %w", dir, mkErr)
		}
		return nil
	default:
		return fmt.Errorf("output directory %q: %w", dir, err)
	}
}

// writeFileExcl streams src into path using O_CREATE|O_EXCL|O_WRONLY
// so an existing file at path returns fs.ErrExist (the caller can
// then bump a version counter and retry). When the io.Copy fails
// mid-stream, the partial file is unlinked so it cannot masquerade
// as a complete artifact.
//
// onWritten, when non-nil, is invoked after each successful Write
// with the cumulative byte count and the chunk just written. It is
// used by copyWithProgress to thread progress events through.
func writeFileExcl(path string, src io.Reader, onWritten func(total int64, justWritten int)) (int64, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return 0, err
	}

	cleanup := func() {
		_ = f.Close()
		_ = os.Remove(path)
	}

	var total int64
	buf := make([]byte, 32*1024)
	for {
		nr, rerr := src.Read(buf)
		if nr > 0 {
			nw, werr := f.Write(buf[:nr])
			if werr != nil {
				cleanup()
				return total, werr
			}
			if nw != nr {
				cleanup()
				return total, io.ErrShortWrite
			}
			total += int64(nw)
			if onWritten != nil {
				onWritten(total, nw)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			cleanup()
			return total, rerr
		}
	}

	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return total, err
	}
	return total, nil
}
