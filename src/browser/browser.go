// Package browser lists, previews-loads, copies and deletes WAV samples
// inside one root folder. Every path is checked to stay inside the root, so
// a bad name or ".." can never reach other files on the Push.
package browser

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/federico-pepe/push-hack-sample-edit/src/audio"
	"github.com/federico-pepe/push-hack-sample-edit/src/wavio"
)

// MaxReadBytes is the biggest file Read will load (about 11 min of stereo 24-bit).
const MaxReadBytes = 200 << 20

// Entry is one folder or WAV file.
type Entry struct {
	Name    string
	Path    string
	Dir     bool
	Size    int64
	ModTime time.Time
	Info    wavio.Info // zero for a folder or an unreadable file
	Bad     bool       // a .wav that could not be read
}

// Seconds is the length of the sample.
func (e Entry) Seconds() float64 {
	if e.Info.Rate == 0 {
		return 0
	}
	return float64(e.Info.Frames) / float64(e.Info.Rate)
}

// Store works inside Root.
type Store struct{ Root string }

// ErrOutside is returned for a path outside the root.
var ErrOutside = errors.New("path is outside the sample folder")

// Resolve cleans p and checks it is the root or inside it.
func (s *Store) Resolve(p string) (string, error) {
	root := filepath.Clean(s.Root)
	p = filepath.Clean(p)
	if p != root && !strings.HasPrefix(p, root+string(filepath.Separator)) {
		return "", ErrOutside
	}
	return p, nil
}

// List returns the folders first, then the WAV files, each sorted by name.
// Hidden files and the recorder's _rec scratch folder are skipped.
func (s *Store) List(dir string) ([]Entry, error) {
	dir, err := s.Resolve(dir)
	if err != nil {
		return nil, err
	}
	des, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var dirs, files []Entry
	for _, de := range des {
		n := de.Name()
		if strings.HasPrefix(n, ".") || n == "_rec" {
			continue
		}
		full := filepath.Join(dir, n)
		fi, err := os.Stat(full) // follows links; a loop or dead link is skipped
		if err != nil {
			continue
		}
		e := Entry{Name: n, Path: full, Size: fi.Size(), ModTime: fi.ModTime()}
		switch {
		case fi.IsDir():
			e.Dir = true
			dirs = append(dirs, e)
		case strings.EqualFold(filepath.Ext(n), ".wav"):
			if in, err := wavio.Probe(full); err == nil {
				e.Info = in
			} else {
				e.Bad = true
			}
			files = append(files, e)
		}
	}
	less := func(a []Entry) func(i, j int) bool {
		return func(i, j int) bool { return strings.ToLower(a[i].Name) < strings.ToLower(a[j].Name) }
	}
	sort.Slice(dirs, less(dirs))
	sort.Slice(files, less(files))
	return append(dirs, files...), nil
}

// Parent is the folder above dir, never above the root.
func (s *Store) Parent(dir string) string {
	d, err := s.Resolve(dir)
	if err != nil || d == filepath.Clean(s.Root) {
		return filepath.Clean(s.Root)
	}
	return filepath.Dir(d)
}

// Read loads a WAV for editing or preview.
func (s *Store) Read(path string) (*audio.Buffer, error) {
	p, err := s.Resolve(path)
	if err != nil {
		return nil, err
	}
	fi, err := os.Stat(p)
	if err != nil {
		return nil, err
	}
	if fi.IsDir() {
		return nil, errors.New("that is a folder")
	}
	if fi.Size() > MaxReadBytes {
		return nil, fmt.Errorf("file too big (%d MB)", fi.Size()>>20)
	}
	return wavio.Read(p)
}

// UniqueName returns a file name in dir that does not exist yet:
// "kick.wav", then "kick copy.wav", "kick copy 2.wav", ...
func UniqueName(dir, name string) string {
	if _, err := os.Lstat(filepath.Join(dir, name)); os.IsNotExist(err) {
		return name
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	cand := stem + " copy" + ext
	for i := 2; ; i++ {
		if _, err := os.Lstat(filepath.Join(dir, cand)); os.IsNotExist(err) {
			return cand
		}
		cand = fmt.Sprintf("%s copy %d%s", stem, i, ext)
	}
}

// Copy copies the file src into folder dstDir under a free name and
// returns the new path. All tags in the file (loops, slices) are kept.
func (s *Store) Copy(src, dstDir string) (string, error) {
	src, err := s.Resolve(src)
	if err != nil {
		return "", err
	}
	dstDir, err = s.Resolve(dstDir)
	if err != nil {
		return "", err
	}
	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close()
	fi, err := in.Stat()
	if err != nil {
		return "", err
	}
	if fi.IsDir() {
		return "", errors.New("folders cannot be copied")
	}
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return "", err
	}
	// O_EXCL: never overwrite, even if another copy races us for the name.
	for try := 0; try < 20; try++ {
		dst := filepath.Join(dstDir, UniqueName(dstDir, filepath.Base(src)))
		out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			os.Remove(dst)
			return "", err
		}
		if err := out.Close(); err != nil {
			os.Remove(dst)
			return "", err
		}
		return dst, nil
	}
	return "", errors.New("could not find a free file name")
}

// Delete removes one WAV file. It refuses folders, the root, other file
// types and anything outside the root.
func (s *Store) Delete(path string) error {
	p, err := s.Resolve(path)
	if err != nil {
		return err
	}
	if p == filepath.Clean(s.Root) {
		return ErrOutside
	}
	if !strings.EqualFold(filepath.Ext(p), ".wav") {
		return errors.New("only .wav files can be deleted here")
	}
	fi, err := os.Lstat(p)
	if err != nil {
		return err
	}
	if fi.IsDir() {
		return errors.New("folders cannot be deleted here")
	}
	return os.Remove(p)
}
