package assessment

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
)

const parserVersion = "5.9.3"

//go:embed parser/typescript.js.gz
var parserArchive []byte

//go:embed parser/LICENSE.txt
var parserLicense []byte

//go:embed parser/manifest.json
var parserManifest []byte

// Materialize only trusted, embedded code in an owned private directory. The
// consumer's cwd, node_modules, NODE_PATH and NODE_OPTIONS never select code.
func materializeParser() (string, func(), error) {
	var manifest struct {
		Version          string `json:"version"`
		SourceSHA256     string `json:"sourceSHA256"`
		CompressedSHA256 string `json:"compressedSHA256"`
		LicenseSHA256    string `json:"licenseSHA256"`
	}
	if json.Unmarshal(parserManifest, &manifest) != nil || manifest.Version != parserVersion || digest(parserArchive) != manifest.CompressedSHA256 || digest(parserLicense) != manifest.LicenseSHA256 {
		return "", nil, errors.New("invalid bundled parser")
	}
	reader, err := gzip.NewReader(bytes.NewReader(parserArchive))
	if err != nil {
		return "", nil, err
	}
	defer reader.Close()
	parser, err := io.ReadAll(io.LimitReader(reader, 10<<20))
	if err != nil || digest(parser) != manifest.SourceSHA256 {
		return "", nil, errors.New("invalid bundled parser content")
	}
	dir, err := os.MkdirTemp("", "9lives-assessment-parser-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	path := filepath.Join(dir, "typescript.cjs")
	if err := os.WriteFile(path, parser, 0600); err != nil {
		cleanup()
		return "", nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, "LICENSE.txt"), parserLicense, 0600); err != nil {
		cleanup()
		return "", nil, err
	}
	return path, cleanup, nil
}
