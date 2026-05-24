package endpoint

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// slugify converts a free-form prompt into a filesystem-safe slug
// suitable for filenames. Lowercase; non-alphanumeric ASCII runs
// collapsed to a single dash; trimmed of leading/trailing dashes;
// capped at slugMaxLen runes. Returns slugFallback when the input
// is empty or contains no slug-eligible runes.
func slugify(s string) string {
	const slugMaxLen = 50
	const slugFallback = "image"
	var b strings.Builder
	b.Grow(len(s))
	prevDash := true // suppress leading dash
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			prevDash = false
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
			prevDash = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	out := strings.TrimRight(b.String(), "-")
	if out == "" {
		return slugFallback
	}
	if len([]rune(out)) > slugMaxLen {
		rs := []rune(out)[:slugMaxLen]
		out = strings.TrimRight(string(rs), "-")
		if out == "" {
			return slugFallback
		}
	}
	return out
}

// mimeToExt maps a MIME media type to a filename extension WITHOUT
// the leading dot. Returns ok=false for unknown types. Handles
// parameterized media types (e.g. "image/png; charset=binary").
func mimeToExt(mime string) (string, bool) {
	mt := strings.TrimSpace(mime)
	if i := strings.Index(mt, ";"); i >= 0 {
		mt = strings.TrimSpace(mt[:i])
	}
	switch strings.ToLower(mt) {
	case "image/png":
		return "png", true
	case "image/jpeg", "image/jpg":
		return "jpg", true
	case "image/webp":
		return "webp", true
	case "image/gif":
		return "gif", true
	case "video/mp4":
		return "mp4", true
	case "video/webm":
		return "webm", true
	case "video/quicktime":
		return "mov", true
	default:
		return "", false
	}
}

// extFromURL extracts a filename extension from the path component
// of a URL, without the leading dot and lowercased. Strips query
// string. Normalizes "jpeg" → "jpg". Returns "" when no extension
// is present or the URL is empty.
func extFromURL(u string) string {
	if u == "" {
		return ""
	}
	if i := strings.IndexByte(u, '?'); i >= 0 {
		u = u[:i]
	}
	ext := strings.ToLower(filepath.Ext(u))
	if ext == "" {
		return ""
	}
	ext = ext[1:]
	if ext == "jpeg" {
		ext = "jpg"
	}
	return ext
}

// pickVersion scans dir for filenames matching {slug}-v{N}.{ext}
// or {slug}-v{N}-{idx}.{ext} (combined regex — both forms
// participate), returns N+1 of the maximum N found (or 1 when none
// exist). Caller is responsible for ensuring dir exists.
//
// Batches that need atomic version coherence (all members share
// one N) call this ONCE per batch — the per-image helper accepts
// an explicit version argument so members do not re-scan and
// drift.
func pickVersion(dir, slug, ext string) (int, error) {
	pat := `^` + regexp.QuoteMeta(slug) + `-v(\d+)(?:-\d+)?\.` + regexp.QuoteMeta(ext) + `$`
	re := regexp.MustCompile(pat)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	max := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		m := re.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		n, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		if n > max {
			max = n
		}
	}
	return max + 1, nil
}

// buildFilename composes the on-disk name for a generated artifact.
// idx == -1 means "not a batch — single result". idx >= 1 means
// "batch member, this is the idx-th".
func buildFilename(slug string, v, idx int, ext string) string {
	if idx < 0 {
		return slug + "-v" + strconv.Itoa(v) + "." + ext
	}
	return slug + "-v" + strconv.Itoa(v) + "-" + strconv.Itoa(idx) + "." + ext
}
