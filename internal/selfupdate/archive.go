package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func extractArchive(data []byte, directory, bundle, binary string) error {
	expected := map[string]int64{binary: maxBinary, "LICENSE": maxLegal, "NOTICE": maxLegal, "THIRD-PARTY-NOTICES.txt": maxLegal}
	seen := map[string]bool{}
	accept := func(name string, dir, regular, executable bool, size int64, source io.Reader) error {
		if strings.HasSuffix(name, "/") && !dir {
			return errors.New("release archive has a non-directory with a directory name")
		}
		name = strings.TrimSuffix(name, "/")
		if seen[name] {
			return errors.New("release archive has duplicate members")
		}
		seen[name] = true
		if name == bundle {
			if !dir {
				return errors.New("release archive has no bundle directory")
			}
			return nil
		}
		file, ok := strings.CutPrefix(name, bundle+"/")
		limit, known := expected[file]
		if !ok || !known || !regular || size <= 0 || size > limit || (file == binary && !executable) {
			return errors.New("release archive contains an unexpected or invalid member")
		}
		mode := os.FileMode(0o644)
		if file == binary {
			mode = 0o755
		}
		target, err := os.OpenFile(filepath.Join(directory, file), os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if err != nil {
			return errors.New("could not stage release files")
		}
		written, copyErr := io.Copy(target, io.LimitReader(source, limit+1))
		syncErr := target.Sync()
		closeErr := target.Close()
		if copyErr != nil || syncErr != nil || closeErr != nil || written != size {
			return errors.New("release archive member is incomplete")
		}
		return nil
	}
	if binary == "9l.exe" {
		archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil || len(archive.File) != 5 {
			return errors.New("release ZIP is malformed or incomplete")
		}
		for _, file := range archive.File {
			if file.UncompressedSize64 > maxBinary {
				return errors.New("release ZIP member is too large")
			}
			reader, err := file.Open()
			if err != nil {
				return errors.New("release ZIP member could not be read")
			}
			err = accept(file.Name, file.FileInfo().IsDir(), file.Mode().IsRegular(), true, int64(file.UncompressedSize64), reader)
			_ = reader.Close()
			if err != nil {
				return err
			}
		}
	} else {
		compressed, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return errors.New("release archive is not gzip")
		}
		defer compressed.Close()
		archive := tar.NewReader(io.LimitReader(compressed, maxBinary+3*maxLegal+(1<<20)))
		for {
			header, err := archive.Next()
			if err == io.EOF {
				break
			}
			if err != nil || len(seen) >= 5 {
				return errors.New("release tar archive is malformed")
			}
			if err := accept(header.Name, header.Typeflag == tar.TypeDir,
				header.Typeflag == tar.TypeReg || header.Typeflag == tar.TypeRegA,
				header.Mode&0o111 != 0, header.Size, archive); err != nil {
				return err
			}
		}
		// Finish reading the gzip footer too: a truncated compressed stream is
		// not a complete archive. Only bounded zero padding may follow tar EOF.
		padding, err := io.ReadAll(io.LimitReader(compressed, (1<<20)+1))
		if err != nil || len(padding) > 1<<20 || len(bytes.Trim(padding, "\x00")) != 0 {
			return errors.New("release archive is truncated or has trailing content")
		}
	}
	if len(seen) != 5 || !seen[bundle] {
		return errors.New("release archive is incomplete")
	}
	return nil
}
