package runner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

// Evidence redaction is intentionally conservative and best-effort. The runner
// never claims arbitrary process output is safe to publish.
var commonSecret = regexp.MustCompile(`(?i)(["']?(?:api[_-]?key|access[_-]?token|auth[_-]?token|token|secret|password)["']?\s*[:=]\s*["']?)([^\s"',}]+)`)
var URLCredentials = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://[^\s/@:]+:)([^\s@/]+)(@)`)

func redact(raw []byte) []byte {
	redacted := commonSecret.ReplaceAll(raw, []byte("${1}[REDACTED]"))
	return URLCredentials.ReplaceAll(redacted, []byte("${1}[REDACTED]${3}"))
}

// RedactText applies evidence redaction to a diagnostic string that leaves the
// runner, such as a provider CLI's failure output.
func RedactText(text string) string { return string(redact([]byte(text))) }

func redactArguments(arguments []string) []string {
	redacted := make([]string, len(arguments))
	for index, argument := range arguments {
		redacted[index] = string(redact([]byte(argument)))
	}
	return redacted
}

func writeEvidence(root string, receipt Receipt, stdout, stderr []byte) (Evidence, error) {
	directory := filepath.Join(root, receipt.RunID, receipt.JobID, receipt.AttemptID)
	evidence := receipt.Evidence
	// Bytes, SHA256 and the file all describe the persisted, redacted output.
	stdout, stderr = redact(stdout), redact(stderr)
	evidence.Artifacts = []ArtifactReference{{Kind: "structured-output", Required: true, Present: len(stdout) > 0, Bytes: int64(len(stdout))}}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return evidence, err
	}
	if len(stdout) > 0 {
		path, sum, err := writeAtomic(directory, "stdout.log", stdout)
		if err != nil {
			return evidence, err
		}
		evidence.StdoutPath, evidence.StdoutSHA256 = path, sum
		evidence.Artifacts[0].Path, evidence.Artifacts[0].SHA256 = path, sum
	}
	if len(stderr) > 0 {
		path, sum, err := writeAtomic(directory, "stderr.log", stderr)
		if err != nil {
			return evidence, err
		}
		evidence.StderrPath, evidence.StderrSHA256 = path, sum
		evidence.Artifacts = append(evidence.Artifacts, ArtifactReference{Kind: "terminal-stderr", Path: path, SHA256: sum, Bytes: int64(len(stderr)), Present: true})
	}
	return evidence, nil
}

func writeReceipt(root string, receipt Receipt) (string, error) {
	directory := filepath.Join(root, receipt.RunID, receipt.JobID, receipt.AttemptID)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", err
	}
	// Keep the on-disk receipt location-independent so it remains portable.
	receipt.ReceiptPath = ""
	raw, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return "", err
	}
	path, _, err := writeAtomic(directory, "receipt.json", append(raw, '\n'))
	return path, err
}

func writeAtomic(directory, name string, content []byte) (string, string, error) {
	path := filepath.Join(directory, name)
	temporary, err := os.CreateTemp(directory, "."+name+"-*")
	if err != nil {
		return "", "", err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if _, err = temporary.Write(content); err == nil {
		err = temporary.Chmod(0o600)
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(temporaryName, path)
	}
	if err != nil {
		return "", "", fmt.Errorf("write %s: %w", name, err)
	}
	sum := sha256.Sum256(content)
	return path, hex.EncodeToString(sum[:]), nil
}
