package container

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
)

// File is a file or directory WriteFiles creates in a container, owned by
// root. Directories (Mode with fs.ModeDir) keep their contents and get the
// mode; files are replaced.
type File struct {
	Path string // absolute
	Mode fs.FileMode
	Data []byte
}

// filesArchive renders files as the tar stream `docker cp - <c>:/` unpacks.
// Every entry is root-owned: docker cp keeps the uid and gid the headers
// carry. Parent directories that are missing are created root-owned, 0755.
func filesArchive(files []File) ([]byte, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, f := range files {
		if !path.IsAbs(f.Path) {
			return nil, fmt.Errorf("container file path %q is not absolute", f.Path)
		}
		hdr := &tar.Header{
			Name: strings.TrimPrefix(path.Clean(f.Path), "/"),
			Mode: int64(f.Mode.Perm()),
			Uid:  0, Gid: 0, Uname: "root", Gname: "root",
			Format: tar.FormatPAX,
		}
		if f.Mode.IsDir() {
			hdr.Typeflag = tar.TypeDir
			hdr.Name += "/"
		} else {
			hdr.Typeflag = tar.TypeReg
			hdr.Size = int64(len(f.Data))
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, err
		}
		if !f.Mode.IsDir() {
			if _, err := tw.Write(f.Data); err != nil {
				return nil, err
			}
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// dirFromArchive returns the regular files directly in the directory the
// archive (`docker cp <c>:<dir> -`) holds, by name.
func dirFromArchive(r io.Reader) (map[string][]byte, error) {
	files := map[string][]byte{}
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return files, nil
		}
		if err != nil {
			return nil, err
		}
		// Entries are "<dir name>/<file>"; deeper ones are not ours.
		_, name, ok := strings.Cut(strings.TrimSuffix(hdr.Name, "/"), "/")
		if !ok || name == "" || strings.Contains(name, "/") || hdr.Typeflag != tar.TypeReg {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(tr, 1<<20))
		if err != nil {
			return nil, err
		}
		files[name] = data
	}
}

// fileFromArchive returns the one regular file the archive
// (`docker cp <c>:<file> -`) holds.
func fileFromArchive(r io.Reader) ([]byte, error) {
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("not a regular file")
		}
		if err != nil {
			return nil, err
		}
		if hdr.Typeflag == tar.TypeReg {
			return io.ReadAll(io.LimitReader(tr, 1<<20))
		}
	}
}
