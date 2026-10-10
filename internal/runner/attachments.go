package runner

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
