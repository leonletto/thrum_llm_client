package endpoint

import (
	"bytes"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type blockingReader struct {
	src   []byte
	chunk int
	pause time.Duration
	pos   int
}

func (r *blockingReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.src) {
		return 0, io.EOF
	}
	if r.pos > 0 {
		time.Sleep(r.pause)
	}
	end := r.pos + r.chunk
	if end > len(r.src) {
		end = len(r.src)
	}
	n := copy(p, r.src[r.pos:end])
	r.pos += n
	return n, nil
}

func TestCopyWithProgress_NoCallback(t *testing.T) {
	src := bytes.NewReader([]byte("hello world"))
	var dst bytes.Buffer
	n, err := copyWithProgress(&dst, src, int64(len("hello world")), nil)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if n != int64(len("hello world")) {
		t.Errorf("got %d; want 11", n)
	}
	if dst.String() != "hello world" {
		t.Errorf("dst = %q; want %q", dst.String(), "hello world")
	}
}

func TestCopyWithProgress_ThrottlesByBytes(t *testing.T) {
	const total = 1024 * 1024
	src := &blockingReader{src: make([]byte, total), chunk: 64 * 1024, pause: time.Millisecond}
	var dst bytes.Buffer
	var events int64
	cb := func(e ProgressEvent) {
		atomic.AddInt64(&events, 1)
	}
	n, err := copyWithProgress(&dst, src, int64(total), cb)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if n != int64(total) {
		t.Errorf("got %d; want %d", n, total)
	}
	got := atomic.LoadInt64(&events)
	if got < 3 || got > 8 {
		t.Errorf("expected 3-8 progress events, got %d", got)
	}
}

func TestCopyWithProgress_LastEventIsComplete(t *testing.T) {
	const total = 300 * 1024
	src := bytes.NewReader(make([]byte, total))
	var dst bytes.Buffer

	var mu sync.Mutex
	var seen []ProgressEvent
	cb := func(e ProgressEvent) {
		mu.Lock()
		seen = append(seen, e)
		mu.Unlock()
	}
	_, err := copyWithProgress(&dst, src, int64(total), cb)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(seen) == 0 {
		t.Fatal("expected at least one event")
	}
	last := seen[len(seen)-1]
	if last.Phase != ProgressComplete {
		t.Errorf("last event phase = %v; want ProgressComplete", last.Phase)
	}
	if last.BytesDone != int64(total) {
		t.Errorf("last event BytesDone = %d; want %d", last.BytesDone, total)
	}
}

func TestCopyWithProgress_UnknownTotal(t *testing.T) {
	const total = 300 * 1024
	src := bytes.NewReader(make([]byte, total))
	var dst bytes.Buffer

	var mu sync.Mutex
	var seen []ProgressEvent
	cb := func(e ProgressEvent) {
		mu.Lock()
		seen = append(seen, e)
		mu.Unlock()
	}
	_, err := copyWithProgress(&dst, src, -1, cb)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	for _, e := range seen {
		if e.Phase == ProgressDownloading && e.Percent != -1 {
			t.Errorf("expected Percent=-1 when total unknown; got %d in event %+v", e.Percent, e)
		}
	}
}

func TestCopyWithProgress_CopyError(t *testing.T) {
	src := &failingReader{good: make([]byte, 8), failAfter: 8}
	var dst bytes.Buffer
	_, err := copyWithProgress(&dst, src, -1, nil)
	if err == nil || !strings.Contains(err.Error(), "network error") {
		t.Errorf("expected wrapped 'network error'; got %v", err)
	}
}
