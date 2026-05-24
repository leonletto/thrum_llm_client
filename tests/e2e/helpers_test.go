//go:build e2e

package e2e_test

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/leonletto/thrum_llm_client/endpoint"
)

// ----- repo root resolution (sync.Once-cached) -----

var (
	repoRootOnce sync.Once
	repoRootVal  string
	repoRootErr  error
)

// repoRoot returns the absolute path to the repository's top-level
// directory. Runs `git rev-parse --show-toplevel` exactly once per
// test process and caches the result.
func repoRoot(t *testing.T) string {
	t.Helper()
	repoRootOnce.Do(func() {
		out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
		if err != nil {
			repoRootErr = fmt.Errorf("git rev-parse --show-toplevel: %w", err)
			return
		}
		repoRootVal = strings.TrimSpace(string(out))
	})
	if repoRootErr != nil {
		t.Fatalf("could not resolve repo root: %v", repoRootErr)
	}
	return repoRootVal
}

// ----- .env loading (sole parser; Makefile does not source .env) -----

var loadDotEnvOnce sync.Once

// loadDotEnv reads <repoRoot>/.env and copies each KEY=VALUE pair into
// the process environment via os.Setenv. Idempotent (safe to call from
// every test). Skips the test if .env is missing. Fatals on a malformed
// line.
//
// Parse rules:
//   - Skip empty lines.
//   - Skip lines whose first non-whitespace char is '#'.
//   - Strip a single trailing '\r' (CRLF tolerance).
//   - Split on the FIRST '=' only. A line with no '=' (after trimming)
//     is malformed.
//   - Trim leading/trailing ASCII whitespace from the key.
//   - Trim trailing ASCII whitespace only from the value (preserve
//     internal characters). No quote stripping; the repo's .env holds
//     unquoted values.
func loadDotEnv(t *testing.T) {
	t.Helper()
	var skipReason string
	loadDotEnvOnce.Do(func() {
		path := filepath.Join(repoRoot(t), ".env")
		f, err := os.Open(path)
		if err != nil {
			if os.IsNotExist(err) {
				skipReason = fmt.Sprintf("missing .env at %s", path)
				return
			}
			t.Fatalf("open %s: %v", path, err)
		}
		defer f.Close()
		scanner := bufio.NewScanner(f)
		// 1 MiB ceiling — well above any plausible .env line but bounded
		// so a pathological file can't OOM the test process.
		scanner.Buffer(make([]byte, 0, 4*1024), 1*1024*1024)
		lineNo := 0
		for scanner.Scan() {
			lineNo++
			line := strings.TrimRight(scanner.Text(), "\r")
			trimmed := strings.TrimLeft(line, " \t")
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			parts := strings.SplitN(line, "=", 2)
			if len(parts) != 2 {
				t.Fatalf("malformed .env line %d: %q (no '=' separator)", lineNo, line)
			}
			key := strings.TrimSpace(parts[0])
			value := strings.TrimRight(parts[1], " \t")
			if key == "" {
				t.Fatalf("malformed .env line %d: %q (empty key)", lineNo, line)
			}
			if err := os.Setenv(key, value); err != nil {
				t.Fatalf("os.Setenv %s: %v", key, err)
			}
		}
		if err := scanner.Err(); err != nil {
			t.Fatalf("scan %s: %v", path, err)
		}
	})
	if skipReason != "" {
		t.Skip(skipReason)
	}
}

// requireEnv loads .env (idempotent) then returns os.Getenv(key).
// Skips the test if the resulting value is empty.
func requireEnv(t *testing.T, key string) string {
	t.Helper()
	loadDotEnv(t)
	v := os.Getenv(key)
	if v == "" {
		t.Skipf("$%s not set", key)
	}
	return v
}

// outputDir returns <repoRoot>/testdata/<provider>/<modality>/ and
// creates it (with parents) if missing. Returns the absolute path.
func outputDir(t *testing.T, provider, modality string) string {
	t.Helper()
	dir := filepath.Join(repoRoot(t), "testdata", provider, modality)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	return dir
}

// progressRecorder returns a thread-safe endpoint.ProgressFn and a
// getter that returns a snapshot of recorded phases in arrival order.
// Safe to share with the library, which may invoke the callback from
// a goroutine during streaming downloads.
func progressRecorder() (endpoint.ProgressFn, func() []endpoint.ProgressPhase) {
	var (
		mu     sync.Mutex
		phases []endpoint.ProgressPhase
	)
	fn := func(ev endpoint.ProgressEvent) {
		mu.Lock()
		phases = append(phases, ev.Phase)
		mu.Unlock()
	}
	getter := func() []endpoint.ProgressPhase {
		mu.Lock()
		defer mu.Unlock()
		out := make([]endpoint.ProgressPhase, len(phases))
		copy(out, phases)
		return out
	}
	return fn, getter
}

// containsPhase reports whether ph appears at least once in phases.
func containsPhase(phases []endpoint.ProgressPhase, ph endpoint.ProgressPhase) bool {
	for _, p := range phases {
		if p == ph {
			return true
		}
	}
	return false
}

// assertDownloadedFile fatals unless path exists, its size is >=
// minBytes, and its lowercase extension is in allowedExts.
func assertDownloadedFile(t *testing.T, path string, minBytes int64, allowedExts ...string) {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat downloaded file %s: %v", path, err)
	}
	if st.Size() < minBytes {
		t.Fatalf("downloaded file %s is %d bytes; want >= %d", path, st.Size(), minBytes)
	}
	ext := strings.ToLower(filepath.Ext(path))
	for _, allowed := range allowedExts {
		if ext == strings.ToLower(allowed) {
			return
		}
	}
	t.Fatalf("downloaded file %s has extension %q; want one of %v", path, ext, allowedExts)
}

// assertSlugNaming verifies filepath.Base(path) matches
// <promptSlug>-v<N>.<ext>. promptSlug MUST be the already-slugified
// form (e.g. "a-red-apple-on-a-wooden-table"), not the raw prompt.
func assertSlugNaming(t *testing.T, path, promptSlug string) {
	t.Helper()
	base := filepath.Base(path)
	re := regexp.MustCompile(`^` + regexp.QuoteMeta(promptSlug) + `-v\d+\.[a-z0-9]+$`)
	if !re.MatchString(base) {
		t.Fatalf("file basename %q does not match expected slug naming %q-v<N>.<ext>", base, promptSlug)
	}
}
