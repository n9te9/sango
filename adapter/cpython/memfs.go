package cpython

import (
	"archive/zip"
	"bytes"
	"io"
	"io/fs"
	"path"
	"sort"
	"time"
)

// memFS is a read-only, seekable fs.FS backed by in-memory buffers.
//
// zip.Reader cannot be used directly as the guest filesystem: files it
// returns are sequential deflate streams and do not implement io.Seeker,
// so guest code that seeks (e.g. reading tzdata or font binaries) fails
// with ENOTSUP. Decompressing up front trades host memory for seekability.
//
// The buffers live on the Go heap, not in the guest's linear memory, so
// they do not affect snapshot size.
type memFS struct {
	files map[string][]byte // path -> content (no leading slash)
	dirs  map[string]bool   // path -> exists ("." is always present)
	times map[string]time.Time
}

func newMemFSFromZip(zr *zip.Reader) (*memFS, error) {
	m := &memFS{
		files: make(map[string][]byte, len(zr.File)),
		dirs:  map[string]bool{".": true},
		times: make(map[string]time.Time, len(zr.File)),
	}

	for _, f := range zr.File {
		name := path.Clean(f.Name)
		if name == "." || !fs.ValidPath(name) {
			continue
		}
		if f.FileInfo().IsDir() {
			m.addDirs(name)
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, err
		}
		m.files[name] = b
		m.times[name] = f.Modified
		m.addDirs(path.Dir(name))
	}
	return m, nil
}

// addDirs registers p and all of its ancestors as directories. Zip archives
// do not reliably contain explicit directory entries, so they are inferred.
func (m *memFS) addDirs(p string) {
	for p != "." && p != "/" && p != "" {
		m.dirs[p] = true
		p = path.Dir(p)
	}
}

func (m *memFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	if b, ok := m.files[name]; ok {
		return &memFile{
			Reader: bytes.NewReader(b),
			info: memInfo{
				name:    path.Base(name),
				size:    int64(len(b)),
				modTime: m.times[name],
			},
		}, nil
	}
	if name == "." || m.dirs[name] {
		return &memDir{fsys: m, path: name}, nil
	}
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}

func (m *memFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrInvalid}
	}
	if name != "." && !m.dirs[name] {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
	}
	seen := make(map[string]bool)
	var out []fs.DirEntry
	add := func(full string, dir bool) {
		if path.Dir(full) != name {
			return
		}
		base := path.Base(full)
		if seen[base] {
			return
		}
		seen[base] = true
		if dir {
			out = append(out, memInfo{name: base, dir: true})
		} else {
			out = append(out, memInfo{
				name: base, size: int64(len(m.files[full])), modTime: m.times[full],
			})
		}
	}
	for p := range m.files {
		add(p, false)
	}
	for p := range m.dirs {
		add(p, true)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out, nil
}

// memFile is seekable via the embedded *bytes.Reader, which also provides
// ReadAt for pread-style access.
type memFile struct {
	*bytes.Reader
	info memInfo
}

func (f *memFile) Stat() (fs.FileInfo, error) { return f.info, nil }
func (f *memFile) Close() error               { return nil }

type memDir struct {
	fsys *memFS
	path string
	ents []fs.DirEntry
	off  int
}

func (d *memDir) Stat() (fs.FileInfo, error) {
	return memInfo{name: path.Base(d.path), dir: true}, nil
}
func (d *memDir) Read([]byte) (int, error) {
	return 0, &fs.PathError{Op: "read", Path: d.path, Err: fs.ErrInvalid}
}
func (d *memDir) Close() error { return nil }

func (d *memDir) ReadDir(n int) ([]fs.DirEntry, error) {
	if d.ents == nil {
		ents, err := d.fsys.ReadDir(d.path)
		if err != nil {
			return nil, err
		}
		d.ents = ents
	}
	if n <= 0 {
		out := d.ents[d.off:]
		d.off = len(d.ents)
		return out, nil
	}
	if d.off >= len(d.ents) {
		return nil, io.EOF
	}
	end := min(d.off+n, len(d.ents))
	out := d.ents[d.off:end]
	d.off = end
	return out, nil
}

// memInfo implements both fs.FileInfo and fs.DirEntry.
type memInfo struct {
	name    string
	size    int64
	dir     bool
	modTime time.Time
}

func (i memInfo) Name() string               { return i.name }
func (i memInfo) Size() int64                { return i.size }
func (i memInfo) IsDir() bool                { return i.dir }
func (i memInfo) ModTime() time.Time         { return i.modTime }
func (i memInfo) Sys() any                   { return nil }
func (i memInfo) Type() fs.FileMode          { return i.Mode().Type() }
func (i memInfo) Info() (fs.FileInfo, error) { return i, nil }

func (i memInfo) Mode() fs.FileMode {
	if i.dir {
		return fs.ModeDir | 0o555
	}
	return 0o444
}
