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

func redact(raw []byte) []byte { return commonSecret.ReplaceAll(raw, []byte("${1}[REDACTED]")) }

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
	evidence.Artifacts = []ArtifactReference{{Kind: "structured-output", Required: true, Present: len(stdout) > 0, Bytes: int64(len(stdout))}}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return evidence, err
	}
	if len(stdout) > 0 {
		path, sum, err := writeAtomic(directory, "stdout.log", redact(stdout))
		if err != nil {
			return evidence, err
		}
		evidence.StdoutPath, evidence.StdoutSHA256 = path, sum
		evidence.Artifacts[0].Path, evidence.Artifacts[0].SHA256 = path, sum
	}
	if len(stderr) > 0 {
		path, sum, err := writeAtomic(directory, "stderr.log", redact(stderr))
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
