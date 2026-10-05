// Package provision puts a machine into the state the configuration describes.
package provision

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
)

// Tar builds a gzipped tar of generated files and whole directories, keyed by
// their path inside the archive.
//
// files carries what was generated in memory — the inventory, the playbook —
// and dirs carries what is copied as it is, such as a recipe's role. The two
// are merged into one archive so a whole tree reaches the machine in a single
// round trip.
//
// Entries are written in sorted order, so the same input gives the same bytes
// and a change to what is sent shows up as a change.
func Tar(files map[string][]byte, dirs map[string]string) (io.Reader, error) {
	var buf bytes.Buffer
	zipped := gzip.NewWriter(&buf)
	archive := tar.NewWriter(zipped)

	for _, name := range slices.Sorted(maps.Keys(files)) {
		header := &tar.Header{
			Name:     name,
			Mode:     0o644,
			Size:     int64(len(files[name])),
			Typeflag: tar.TypeReg,
		}
		if err := archive.WriteHeader(header); err != nil {
			return nil, fmt.Errorf("writing the header for %s: %w", name, err)
		}
		if _, err := archive.Write(files[name]); err != nil {
			return nil, fmt.Errorf("writing %s: %w", name, err)
		}
	}

	for _, prefix := range slices.Sorted(maps.Keys(dirs)) {
		if err := addDir(archive, prefix, dirs[prefix]); err != nil {
			return nil, err
		}
	}

	if err := archive.Close(); err != nil {
		return nil, fmt.Errorf("closing the archive: %w", err)
	}
	if err := zipped.Close(); err != nil {
		return nil, fmt.Errorf("closing the archive: %w", err)
	}
	return &buf, nil
}

func addDir(archive *tar.Writer, prefix, source string) error {
	if _, err := os.Stat(source); err != nil {
		return fmt.Errorf("reading %s: %w", source, err)
	}
	// WalkDir does not follow a root that is a symlink, as a local package
	// linked into the configuration's packages directory is.
	source, err := filepath.EvalSymlinks(source)
	if err != nil {
		return fmt.Errorf("resolving %s: %w", source, err)
	}

	return filepath.WalkDir(source, func(current string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, current)
		if err != nil {
			return err
		}
		name := prefix
		if relative != "." {
			name = path.Join(prefix, filepath.ToSlash(relative))
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}
		// The mode travels with the entry, so a role that ships a script still
		// has one on the other side.
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return fmt.Errorf("describing %s: %w", current, err)
		}
		header.Name = name

		switch {
		case entry.IsDir():
			header.Name = name + "/"
		case !info.Mode().IsRegular():
			return fmt.Errorf("%s is neither a file nor a directory, and cannot be sent", current)
		}

		if err := archive.WriteHeader(header); err != nil {
			return fmt.Errorf("writing the header for %s: %w", name, err)
		}
		if entry.IsDir() {
			return nil
		}

		body, err := os.Open(current)
		if err != nil {
			return err
		}
		defer body.Close()
		if _, err := io.Copy(archive, body); err != nil {
			return fmt.Errorf("writing %s: %w", name, err)
		}
		return nil
	})
}
