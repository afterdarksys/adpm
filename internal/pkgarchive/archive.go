// Package pkgarchive implements the portable archive formats used by ADPM.
package pkgarchive

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/bzip2"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type readCloser struct {
	io.Reader
	closers []io.Closer
}

type commandReader struct {
	io.ReadCloser
	cmd *exec.Cmd
}

func (r *commandReader) Close() error { _ = r.ReadCloser.Close(); return r.cmd.Wait() }

func (r *readCloser) Close() error {
	for i := len(r.closers) - 1; i >= 0; i-- {
		if err := r.closers[i].Close(); err != nil {
			return err
		}
	}
	return nil
}

// OpenPayload opens a raw, gzip, or bzip2-compressed archive payload.
func OpenPayload(path string) (io.ReadCloser, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	br := bufio.NewReader(f)
	magic, _ := br.Peek(6)
	r := &readCloser{Reader: br, closers: []io.Closer{f}}
	if len(magic) >= 2 && magic[0] == 0x1f && magic[1] == 0x8b {
		gz, err := gzip.NewReader(br)
		if err != nil {
			f.Close()
			return nil, err
		}
		r.Reader, r.closers = gz, []io.Closer{gz, f}
	} else if len(magic) >= 3 && string(magic[:3]) == "BZh" {
		r.Reader = bzip2.NewReader(br)
	} else if len(magic) == 6 && bytes.Equal(magic, []byte{0xfd, '7', 'z', 'X', 'Z', 0x00}) {
		_ = f.Close()
		cmd := exec.Command("xz", "-dc", path)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return nil, err
		}
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			return nil, err
		}
		return &commandReader{ReadCloser: stdout, cmd: cmd}, nil
	} else if len(magic) >= 4 && bytes.Equal(magic[:4], []byte{0x28, 0xb5, 0x2f, 0xfd}) {
		_ = f.Close()
		cmd := exec.Command("zstd", "-dc", path)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return nil, err
		}
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			return nil, fmt.Errorf("zstd is required to read this archive: %w", err)
		}
		return &commandReader{ReadCloser: stdout, cmd: cmd}, nil
	}
	return r, nil
}

// ExtractAuto safely extracts a tar or SVR4 newc CPIO payload.
func ExtractAuto(path, dest string) error {
	r, err := OpenPayload(path)
	if err != nil {
		return err
	}
	defer r.Close()
	br := bufio.NewReader(r)
	magic, _ := br.Peek(6)
	if string(magic) == "070701" || string(magic) == "070702" {
		return extractCPIO(br, dest)
	}
	if string(magic) == "070707" {
		return extractODC(br, dest)
	}
	return extractTar(br, dest)
}

func safeTarget(root, name string) (string, error) {
	root, _ = filepath.Abs(root)
	name = filepath.Clean(filepath.FromSlash(strings.TrimPrefix(name, "./")))
	if name == "." {
		return root, nil
	}
	if filepath.IsAbs(name) || name == ".." || strings.HasPrefix(name, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("unsafe archive path %q", name)
	}
	target := filepath.Join(root, name)
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("archive path escapes destination: %q", name)
	}
	current := root
	for _, part := range strings.Split(rel, string(os.PathSeparator)) {
		if part == "." || part == "" {
			continue
		}
		current = filepath.Join(current, part)
		if info, statErr := os.Lstat(current); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("archive path traverses symlink: %q", name)
		}
	}
	return target, nil
}

func validateLink(root, name, link string) error {
	if filepath.IsAbs(link) {
		return fmt.Errorf("unsafe absolute symlink %q", name)
	}
	_, err := safeTarget(root, filepath.Join(filepath.Dir(name), link))
	return err
}

func extractTar(r io.Reader, dest string) error {
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read tar: %w", err)
		}
		target, err := safeTarget(dest, h.Name)
		if err != nil {
			return err
		}
		switch h.Typeflag {
		case tar.TypeDir:
			err = os.MkdirAll(target, os.FileMode(h.Mode)&0777)
		case tar.TypeReg, tar.TypeRegA:
			if err = os.MkdirAll(filepath.Dir(target), 0755); err == nil {
				var f *os.File
				f, err = os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(h.Mode)&0777)
				if err == nil {
					_, err = io.CopyN(f, tr, h.Size)
					closeErr := f.Close()
					if err == nil {
						err = closeErr
					}
				}
			}
		case tar.TypeSymlink:
			if err = validateLink(dest, h.Name, h.Linkname); err != nil {
				return err
			}
			if err = os.MkdirAll(filepath.Dir(target), 0755); err == nil {
				err = os.Symlink(h.Linkname, target)
			}
		default:
			continue
		}
		if err != nil {
			return fmt.Errorf("extract %s: %w", h.Name, err)
		}
	}
}

func parseHex(b []byte) (uint64, error) { return strconv.ParseUint(string(b), 16, 64) }
func parseOctal(b []byte) (uint64, error) {
	return strconv.ParseUint(strings.TrimSpace(string(b)), 8, 64)
}
func skipPad(r io.Reader, n int64) error {
	if p := (4 - n%4) % 4; p > 0 {
		_, err := io.CopyN(io.Discard, r, p)
		return err
	}
	return nil
}

// extractODC reads the portable ASCII CPIO format emitted by older ADPM builders.
func extractODC(r io.Reader, dest string) error {
	header := make([]byte, 76)
	for {
		if _, err := io.ReadFull(r, header); err != nil {
			return fmt.Errorf("read odc cpio header: %w", err)
		}
		if string(header[:6]) != "070707" {
			return fmt.Errorf("unsupported odc cpio magic %q", header[:6])
		}
		mode, err := parseOctal(header[18:24])
		if err != nil {
			return err
		}
		mtime, _ := parseOctal(header[48:59])
		nameSize, err := parseOctal(header[59:65])
		if err != nil || nameSize == 0 {
			return fmt.Errorf("invalid odc cpio name size")
		}
		size, err := parseOctal(header[65:76])
		if err != nil {
			return err
		}
		nameBytes := make([]byte, nameSize)
		if _, err := io.ReadFull(r, nameBytes); err != nil {
			return err
		}
		name := strings.TrimSuffix(string(nameBytes), "\x00")
		if name == "TRAILER!!!" {
			return nil
		}
		target, err := safeTarget(dest, name)
		if err != nil {
			return err
		}
		kind := mode & 0170000
		switch kind {
		case 0040000:
			err = os.MkdirAll(target, os.FileMode(mode)&0777)
			if size > 0 {
				_, err = io.CopyN(io.Discard, r, int64(size))
			}
		case 0120000:
			data := make([]byte, size)
			if _, err = io.ReadFull(r, data); err == nil {
				link := string(data)
				if err = validateLink(dest, name, link); err != nil {
					return err
				}
				err = os.MkdirAll(filepath.Dir(target), 0755)
				if err == nil {
					err = os.Symlink(link, target)
				}
			}
		default:
			if err = os.MkdirAll(filepath.Dir(target), 0755); err == nil {
				var f *os.File
				f, err = os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(mode)&0777)
				if err == nil {
					_, err = io.CopyN(f, r, int64(size))
					closeErr := f.Close()
					if err == nil {
						err = closeErr
					}
				}
			}
		}
		if err != nil {
			return fmt.Errorf("extract %s: %w", name, err)
		}
		if mtime > 0 && kind != 0120000 {
			_ = os.Chtimes(target, time.Unix(int64(mtime), 0), time.Unix(int64(mtime), 0))
		}
	}
}

func extractCPIO(r io.Reader, dest string) error {
	header := make([]byte, 110)
	for {
		if _, err := io.ReadFull(r, header); err != nil {
			return fmt.Errorf("read cpio header: %w", err)
		}
		magic := string(header[:6])
		if magic != "070701" && magic != "070702" {
			return fmt.Errorf("unsupported cpio magic %q", magic)
		}
		mode, err := parseHex(header[14:22])
		if err != nil {
			return err
		}
		mtime, _ := parseHex(header[46:54])
		size, err := parseHex(header[54:62])
		if err != nil {
			return err
		}
		nameSize, err := parseHex(header[94:102])
		if err != nil || nameSize == 0 {
			return fmt.Errorf("invalid cpio name size")
		}
		nameBytes := make([]byte, nameSize)
		if _, err := io.ReadFull(r, nameBytes); err != nil {
			return err
		}
		if err := skipPad(r, int64(110+nameSize)); err != nil {
			return err
		}
		name := strings.TrimSuffix(string(nameBytes), "\x00")
		if name == "TRAILER!!!" {
			return nil
		}
		target, err := safeTarget(dest, name)
		if err != nil {
			return err
		}
		kind := mode & 0170000
		switch kind {
		case 0040000:
			err = os.MkdirAll(target, os.FileMode(mode)&0777)
		case 0120000:
			data := make([]byte, size)
			if _, err = io.ReadFull(r, data); err == nil {
				link := string(data)
				if err = validateLink(dest, name, link); err != nil {
					return err
				}
				err = os.MkdirAll(filepath.Dir(target), 0755)
				if err == nil {
					err = os.Symlink(link, target)
				}
			}
		default:
			if err = os.MkdirAll(filepath.Dir(target), 0755); err == nil {
				var f *os.File
				f, err = os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(mode)&0777)
				if err == nil {
					_, err = io.CopyN(f, r, int64(size))
					closeErr := f.Close()
					if err == nil {
						err = closeErr
					}
				}
			}
		}
		if kind == 0040000 {
			if size > 0 {
				_, err = io.CopyN(io.Discard, r, int64(size))
			}
		}
		if err != nil {
			return fmt.Errorf("extract %s: %w", name, err)
		}
		if err := skipPad(r, int64(size)); err != nil {
			return err
		}
		if mtime > 0 && kind != 0120000 {
			_ = os.Chtimes(target, time.Unix(int64(mtime), 0), time.Unix(int64(mtime), 0))
		}
	}
}

// WriteTarGz creates a deterministic gzip-compressed tar archive.
func WriteTarGz(root, output string) error {
	f, err := os.Create(output)
	if err != nil {
		return err
	}
	gz := gzip.NewWriter(f)
	gz.Name = ""
	gz.ModTime = time.Unix(0, 0)
	tw := tar.NewWriter(gz)
	err = walkSorted(root, func(path, name string, info os.FileInfo) error {
		link := ""
		if info.Mode()&os.ModeSymlink != 0 {
			link, _ = os.Readlink(path)
		}
		h, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return err
		}
		h.Name = filepath.ToSlash(name)
		h.ModTime = time.Unix(0, 0)
		if err := tw.WriteHeader(h); err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			in, err := os.Open(path)
			if err != nil {
				return err
			}
			_, err = io.Copy(tw, in)
			in.Close()
			return err
		}
		return nil
	})
	if closeErr := tw.Close(); err == nil {
		err = closeErr
	}
	if closeErr := gz.Close(); err == nil {
		err = closeErr
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	return err
}

func walkSorted(root string, fn func(path, name string, info os.FileInfo) error) error {
	var paths []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if path != root {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	sort.Strings(paths)
	for _, path := range paths {
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		name, _ := filepath.Rel(root, path)
		if err := fn(path, name, info); err != nil {
			return err
		}
	}
	return nil
}

// WriteCPIO writes an SVR4 newc CPIO archive, optionally compressed with gzip or bzip2.
func WriteCPIO(root, output, compression string) error {
	tmp, err := os.CreateTemp(filepath.Dir(output), ".adpm-*.cpio")
	if err != nil {
		return err
	}
	raw := tmp.Name()
	defer os.Remove(raw)
	ino := uint64(1)
	writeEntry := func(name string, info os.FileInfo, data io.Reader, size int64) error {
		mode := uint64(info.Mode().Perm())
		switch {
		case info.IsDir():
			mode |= 0040000
		case info.Mode()&os.ModeSymlink != 0:
			mode |= 0120000
		default:
			mode |= 0100000
		}
		name = filepath.ToSlash(name)
		ns := len(name) + 1
		header := fmt.Sprintf("070701%08x%08x%08x%08x%08x%08x%08x%08x%08x%08x%08x%08x%08x", ino, mode, 0, 0, 1, 0, size, 0, 0, 0, 0, ns, 0)
		ino++
		if _, err := io.WriteString(tmp, header+name+"\x00"); err != nil {
			return err
		}
		if p := (4 - (110+ns)%4) % 4; p > 0 {
			_, _ = tmp.Write(make([]byte, p))
		}
		if data != nil {
			if _, err := io.CopyN(tmp, data, size); err != nil {
				return err
			}
		}
		if p := (4 - int(size)%4) % 4; p > 0 {
			_, _ = tmp.Write(make([]byte, p))
		}
		return nil
	}
	err = walkSorted(root, func(path, name string, info os.FileInfo) error {
		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return writeEntry(name, info, strings.NewReader(link), int64(len(link)))
		}
		if info.Mode().IsRegular() {
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			defer f.Close()
			return writeEntry(name, info, f, info.Size())
		}
		return writeEntry(name, info, nil, 0)
	})
	if err == nil {
		trailer, _ := os.Stat(root)
		err = writeEntry("TRAILER!!!", trailer, nil, 0)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	switch compression {
	case "", "none", "raw":
		return copyFile(raw, output)
	case "gzip", "gz":
		in, err := os.Open(raw)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.Create(output)
		if err != nil {
			return err
		}
		gz := gzip.NewWriter(out)
		gz.Name = ""
		gz.ModTime = time.Unix(0, 0)
		_, err = io.Copy(gz, in)
		if e := gz.Close(); err == nil {
			err = e
		}
		if e := out.Close(); err == nil {
			err = e
		}
		return err
	case "bzip2", "bz2":
		out, err := os.Create(output)
		if err != nil {
			return err
		}
		cmd := exec.Command("bzip2", "-9", "-c", raw)
		cmd.Stdout = out
		cmd.Stderr = os.Stderr
		err = cmd.Run()
		if e := out.Close(); err == nil {
			err = e
		}
		return err
	default:
		return fmt.Errorf("unsupported cpio compression %q", compression)
	}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if e := out.Close(); err == nil {
		err = e
	}
	return err
}
