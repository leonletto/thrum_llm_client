package endpoint

import (
	"io"
	"time"
)

// progressByteThreshold is the minimum number of bytes between two
// successive ProgressDownloading events emitted by copyWithProgress.
const progressByteThreshold = 256 * 1024

// progressTimeThreshold is the minimum wall-clock interval between
// two successive ProgressDownloading events. Either threshold can
// trigger an emission — whichever fires first.
const progressTimeThreshold = 200 * time.Millisecond

// copyWithProgress streams src into dst, returning the total bytes
// copied. When onProgress is non-nil it is invoked at most every
// progressByteThreshold OR every progressTimeThreshold (whichever
// fires first) with a ProgressDownloading event, plus exactly one
// ProgressComplete event at the end of a successful copy. On copy
// failure no ProgressComplete is emitted and the error from io.Copy
// is returned.
//
// totalBytes is the expected total size, used to populate
// ProgressEvent.BytesTotal and Percent. Pass -1 when unknown
// (e.g. server did not send Content-Length).
func copyWithProgress(dst io.Writer, src io.Reader, totalBytes int64, onProgress ProgressFn) (int64, error) {
	if onProgress == nil {
		return io.Copy(dst, src)
	}

	var (
		written      int64
		lastEmitted  int64
		lastEmitTime = time.Now()
	)
	buf := make([]byte, 64*1024)
	for {
		nr, rerr := src.Read(buf)
		if nr > 0 {
			nw, werr := dst.Write(buf[:nr])
			if werr != nil {
				return written, werr
			}
			if nw != nr {
				return written, io.ErrShortWrite
			}
			written += int64(nw)
			now := time.Now()
			if written-lastEmitted >= progressByteThreshold || now.Sub(lastEmitTime) >= progressTimeThreshold {
				onProgress(ProgressEvent{
					Phase:      ProgressDownloading,
					Percent:    percentOf(written, totalBytes),
					BytesDone:  written,
					BytesTotal: totalBytes,
				})
				lastEmitted = written
				lastEmitTime = now
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return written, rerr
		}
	}
	onProgress(ProgressEvent{
		Phase:      ProgressComplete,
		Percent:    100,
		BytesDone:  written,
		BytesTotal: totalBytes,
	})
	return written, nil
}

// percentOf returns the integer percentage of done over total in
// [0, 100], or -1 when total is unknown (< 0). When done > total
// (rare — server lied about Content-Length), caps at 100.
func percentOf(done, total int64) int {
	if total <= 0 {
		return -1
	}
	if done >= total {
		return 100
	}
	return int((done * 100) / total)
}
