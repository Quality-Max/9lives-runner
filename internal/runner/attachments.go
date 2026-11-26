package runner

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	maxRetainedAttachmentBytes = 32 << 20
	maxRetainedAttemptBytes    = 128 << 20
)

// retainAttachments copies each reported attachment into directory, so the
// project's next run cannot delete it. Text attachments are redacted like
// other evidence. A file that is missing, not regular, or over the size
// limits stays a reference with Retained false. Adapters report only paths
// inside the job's working directory.
func retainAttachments(directory string, failures []TestFailure) {
	var total int64
	copied := 0
	for i := range failures {
		for j := range failures[i].Attachments {
			attachment := &failures[i].Attachments[j]
			info, err := os.Lstat(attachment.Path)
			if err != nil || !info.Mode().IsRegular() || info.Size() > maxRetainedAttachmentBytes || total+info.Size() > maxRetainedAttemptBytes {
				continue
			}
			if os.MkdirAll(directory, 0o700) != nil {
				return
			}
			copied++
			name := fmt.Sprintf("%02d-%s", copied, safeAttachmentName(filepath.Base(attachment.Path)))
			path, sum, size, err := copyAttachment(attachment.Path, filepath.Join(directory, name), textAttachment(attachment.ContentType))
			if err != nil {
				continue
			}
			total += size
			attachment.Path, attachment.SHA256, attachment.Bytes, attachment.Retained = path, sum, size, true
		}
	}
}

// FailureCaptureKeptEnv tells a target worker whether this attempt retains
// an opted-in failure capture: "1" or "0", set by the engine per attempt.
const FailureCaptureKeptEnv = "NINELIVES_FAILURE_CAPTURE_KEPT"

// The files an Android failure capture leaves in Playwright's output: the
// SDK's reserved attachment name, its content type, and the name Playwright
// gives the copy (`attachments/<sanitized name>-<sha1><extension>`).
var nativeCaptures = map[string]struct {
	contentType string
	file        *regexp.Regexp
}{
	"9l.failure-screenshot": {"image/png", regexp.MustCompile(`^9l-failure-screenshot-[0-9a-f]{40}\.png$`)},
	"9l.failure-view-tree":  {"text/xml", regexp.MustCompile(`^9l-failure-view-tree-[0-9a-f]{40}\.xml$`)},
}

// keepNativeCaptures removes every attachment under output except a failure
// capture's two files, at most one of each per failure. Attachments outside
// output are left as they are.
func keepNativeCaptures(failures []TestFailure, output string) {
	for i := range failures {
		kept, seen := failures[i].Attachments[:0], map[string]bool{}
		for _, attachment := range failures[i].Attachments {
			if !insideDirectory(output, attachment.Path) {
				kept = append(kept, attachment)
				continue
			}
			capture, ok := nativeCaptures[attachment.Name]
			if !ok || seen[attachment.Name] || attachment.ContentType != capture.contentType ||
				filepath.Base(filepath.Dir(attachment.Path)) != "attachments" || !capture.file.MatchString(filepath.Base(attachment.Path)) {
				continue
			}
			seen[attachment.Name] = true
			kept = append(kept, attachment)
		}
		if len(kept) == 0 {
			kept = nil
		}
		failures[i].Attachments = kept
	}
}

func insideDirectory(directory, path string) bool {
	relative, err := filepath.Rel(directory, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

// dropScratchReferences removes the attachments under scratch that were not
// retained: the scratch is deleted with the attempt, so a reference to it
// would name a file that no longer exists.
func dropScratchReferences(failures []TestFailure, scratch string) {
	for i := range failures {
		kept := failures[i].Attachments[:0]
		for _, attachment := range failures[i].Attachments {
			if attachment.Retained || !insideDirectory(scratch, attachment.Path) {
				kept = append(kept, attachment)
			}
		}
		if len(kept) == 0 {
			kept = nil
		}
		failures[i].Attachments = kept
	}
}

func copyAttachment(source, target string, text bool) (string, string, int64, error) {
	input, err := os.Open(source)
	if err != nil {
		return "", "", 0, err
	}
	defer input.Close()
	var contents []byte
	if text {
		raw, err := io.ReadAll(io.LimitReader(input, maxRetainedAttachmentBytes+1))
		if err != nil || len(raw) > maxRetainedAttachmentBytes {
			return "", "", 0, fmt.Errorf("attachment unreadable or too large")
		}
		contents = redact(raw)
		path, sum, err := writeAtomic(filepath.Dir(target), filepath.Base(target), contents)
		return path, sum, int64(len(contents)), err
	}
	output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", "", 0, err
	}
	hash := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(output, hash), io.LimitReader(input, maxRetainedAttachmentBytes+1))
	closeErr := output.Close()
	if copyErr != nil || closeErr != nil || size > maxRetainedAttachmentBytes {
		_ = os.Remove(target)
		return "", "", 0, fmt.Errorf("attachment could not be copied")
	}
	return target, hex.EncodeToString(hash.Sum(nil)), size, nil
}

func textAttachment(contentType string) bool {
	contentType = strings.ToLower(contentType)
	return strings.HasPrefix(contentType, "text/") || strings.Contains(contentType, "json")
}

func safeAttachmentName(name string) string {
	clean := strings.Map(func(r rune) rune {
		if r == '.' || r == '-' || r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}
		return '_'
	}, name)
	if len(clean) > 80 {
		clean = clean[len(clean)-80:]
	}
	if clean == "" || clean == "." || clean == ".." {
		return "attachment"
	}
	return clean
}
