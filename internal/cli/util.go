package cli

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
)

// sprintf is fmt.Sprintf under a shorter name, used heavily in prompts.
func sprintf(format string, args ...any) string { return fmt.Sprintf(format, args...) }

// errString builds a plain error from a literal message.
func errString(msg string) error { return errors.New(msg) }

func itoa(n int) string { return strconv.Itoa(n) }

// truncate shortens s to at most n runes, marking elision with an ellipsis.
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	if n <= 1 {
		return string(runes[:n])
	}
	return string(runes[:n-1]) + "…"
}

// slug turns a display name into an identifier safe for URLs and filenames.
func slug(s string) string {
	var b strings.Builder
	lastDash := true // leading dashes are suppressed
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastDash = false
		case !lastDash:
			b.WriteRune('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// splitList parses a comma-separated flag value into trimmed, non-empty parts.
func splitList(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// tarDirectory writes a directory tree into an uncompressed tar archive.
//
// Uncompressed on purpose: attachment blobs are overwhelmingly already-
// compressed formats — PDFs, JPEGs, zips — so gzip would burn CPU across
// gigabytes to save very little, and an uncompressed tar can be inspected and
// extracted with anything.
func tarDirectory(root, dest string) error {
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("%s already exists", dest)
	}

	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()

	writer := tar.NewWriter(out)
	defer writer.Close()

	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(rel)
		if err := writer.WriteHeader(header); err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(writer, f)
		return err
	})
}

// plural picks the right noun for a count.
//
// Generic over the integer types actually in use, because the two call sites
// had int and int64 and the alternative was a conversion at each one.
func plural[T ~int | ~int64](n T, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// count renders a number with its noun: "1 message", "3 messages".
//
// Replaces the "%d message(s)" construction in output people see constantly;
// the parenthesised plural reads as unfinished.
func count[T ~int | ~int64](n T, one, many string) string {
	return fmt.Sprintf("%d %s", n, plural(n, one, many))
}

// envBool reads a boolean setting from the environment.
//
// Accepts the spellings people actually type. Anything unrecognised is false:
// a setting that turns off an authentication requirement must never be enabled
// by a typo.
func envBool(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
