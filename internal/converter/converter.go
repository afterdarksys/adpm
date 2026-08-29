package converter

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/afterdarksys/adpm/internal/pkgarchive"
)

type ConversionOptions struct {
	InPkg, Input, OutPkg, Output string
	Name, Version, Architecture  string
}

type metadata struct {
	Name         string                 `json:"name"`
	Version      string                 `json:"version"`
	Description  string                 `json:"description,omitempty"`
	Packager     string                 `json:"packager"`
	Platforms    []string               `json:"platforms,omitempty"`
	SourceFormat string                 `json:"source_format,omitempty"`
	Dependencies map[string]interface{} `json:"dependencies"`
	Install      map[string]interface{} `json:"install"`
	Provenance   map[string]interface{} `json:"provenance,omitempty"`
}

var aliases = map[string]string{
	"tar.gz": "tgz", ".tar.gz": "tgz", "tgz": "tgz",
	"cpio": "cpio", "cpio.gz": "cpio.gz", "cpio.gzip": "cpio.gz",
	"cpio.bz2": "cpio.bz2", "cpio.bzip2": "cpio.bz2",
	"adpm": "adpm", "bpm": "bpm", "rpm": "rpm", "deb": "deb",
}

func Formats() []string {
	return []string{"rpm", "deb", "tgz", "bpm", "adpm", "cpio", "cpio.gz", "cpio.bz2"}
}

func normalize(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if v, ok := aliases[s]; ok {
		return v, nil
	}
	return "", fmt.Errorf("unsupported package format %q (supported: %s)", s, strings.Join(Formats(), ", "))
}

func Convert(opts ConversionOptions) error {
	inFmt, err := normalize(opts.InPkg)
	if err != nil {
		return err
	}
	outFmt, err := normalize(opts.OutPkg)
	if err != nil {
		return err
	}
	input, err := filepath.Abs(opts.Input)
	if err != nil {
		return err
	}
	if info, err := os.Stat(input); err != nil || info.IsDir() {
		return fmt.Errorf("input package is not a readable file: %s", input)
	}
	stage, err := os.MkdirTemp("", "adpm-convert-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)

	inputHash, err := fileSHA256(input)
	if err != nil {
		return err
	}
	meta := metadata{Name: opts.Name, Version: opts.Version, Packager: "After Dark Systems Package Manager", SourceFormat: inFmt, Dependencies: map[string]interface{}{}, Install: map[string]interface{}{"requires_root": false, "install_prefix": "~/.local"}, Provenance: map[string]interface{}{
		"method": "adpm-convert", "source_kind": "package", "source_format": inFmt,
		"source_path": input, "source_sha256": inputHash,
		"converted_at": time.Now().UTC().Truncate(time.Second).Format(time.RFC3339),
	}}
	if err := extract(input, inFmt, stage, &meta); err != nil {
		return fmt.Errorf("extract %s: %w", inFmt, err)
	}
	if meta.Name == "" || meta.Version == "" {
		inferNameVersion(input, &meta)
	}
	if opts.Name != "" {
		meta.Name = opts.Name
	}
	if opts.Version != "" {
		meta.Version = opts.Version
	}
	identity := regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+\-]*$`)
	if !identity.MatchString(meta.Name) || !identity.MatchString(meta.Version) {
		return fmt.Errorf("unsafe package name or version: %q %q (use --name/--version to override)", meta.Name, meta.Version)
	}
	platform := opts.Architecture
	if platform == "" {
		platform = detectPlatform()
	}
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+\-]*$`).MatchString(platform) {
		return fmt.Errorf("invalid target platform %q", platform)
	}
	if outFmt == "adpm" {
		if err := prepareADPM(stage, &meta, platform); err != nil {
			return err
		}
	}
	emitStage := stage
	if outFmt == "rpm" || outFmt == "deb" || (outFmt == "tgz" && inFmt == "adpm") {
		emitStage, err = prepareNative(stage, platform)
		if err != nil {
			return err
		}
		if emitStage != stage {
			defer os.RemoveAll(emitStage)
		}
	}
	output, err := outputPath(opts.Output, meta.Name, meta.Version, outFmt)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(output), 0755); err != nil {
		return err
	}
	fmt.Printf("Converting %s (%s) -> %s (%s)\n", input, inFmt, output, outFmt)
	if err := emit(emitStage, output, outFmt, meta, platform); err != nil {
		return fmt.Errorf("write %s: %w", outFmt, err)
	}
	fmt.Printf("Created %s\n", output)
	return nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", digest.Sum(nil)), nil
}

func extract(input, format, stage string, meta *metadata) error {
	switch format {
	case "tgz", "bpm", "adpm", "cpio", "cpio.gz", "cpio.bz2":
		if err := pkgarchive.ExtractAuto(input, stage); err != nil {
			return err
		}
		readADPMMetadata(stage, meta)
		return nil
	case "rpm":
		name, version, _ := rpmMetadata(input)
		if meta.Name == "" {
			meta.Name = name
		}
		if meta.Version == "" {
			meta.Version = version
		}
		payload, err := os.CreateTemp("", "adpm-rpm-*.cpio")
		if err != nil {
			return err
		}
		payloadPath := payload.Name()
		defer os.Remove(payloadPath)
		producer := exec.Command("rpm2cpio", input)
		producer.Stdout = payload
		producer.Stderr = os.Stderr
		if err = producer.Run(); err != nil {
			payload.Close()
			return fmt.Errorf("rpm2cpio: %w", err)
		}
		if err = payload.Close(); err != nil {
			return err
		}
		return pkgarchive.ExtractAuto(payloadPath, stage)
	case "deb":
		name, version, _ := debMetadata(input)
		if meta.Name == "" {
			meta.Name = name
		}
		if meta.Version == "" {
			meta.Version = version
		}
		if _, err := exec.LookPath("dpkg-deb"); err == nil {
			cmd := exec.Command("dpkg-deb", "-x", input, stage)
			cmd.Stderr = os.Stderr
			return cmd.Run()
		}
		work, err := os.MkdirTemp("", "adpm-deb-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(work)
		cmd := exec.Command("ar", "x", input)
		cmd.Dir = work
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("ar: %s: %w", out, err)
		}
		files, _ := filepath.Glob(filepath.Join(work, "data.tar*"))
		if len(files) == 0 {
			return fmt.Errorf("DEB has no data archive")
		}
		return pkgarchive.ExtractAuto(files[0], stage)
	}
	return fmt.Errorf("unsupported input %s", format)
}

func readADPMMetadata(stage string, meta *metadata) {
	b, err := os.ReadFile(filepath.Join(stage, "META.json"))
	if err != nil {
		return
	}
	var existing metadata
	if json.Unmarshal(b, &existing) != nil {
		return
	}
	if meta.Name == "" {
		meta.Name = existing.Name
	}
	if meta.Version == "" {
		meta.Version = existing.Version
	}
	if existing.Description != "" {
		meta.Description = existing.Description
	}
	if len(existing.Platforms) > 0 {
		meta.Platforms = existing.Platforms
	}
	if existing.Provenance != nil {
		meta.Provenance = existing.Provenance
	}
}

func inferNameVersion(input string, meta *metadata) {
	base := filepath.Base(input)
	for _, suffix := range []string{".cpio.bz2", ".cpio.gz", ".tar.gz", ".adpm", ".bpm", ".tgz", ".rpm", ".deb", ".cpio", ".bz2", ".gz"} {
		base = strings.TrimSuffix(base, suffix)
	}
	if meta.Name == "" {
		meta.Name = base
	}
	if meta.Version == "" {
		meta.Version = "1.0.0"
	}
}

func detectPlatform() string {
	arch := runtime.GOARCH
	if arch == "amd64" {
		arch = "x86_64"
	}
	if arch == "arm64" {
		arch = "aarch64"
	}
	return runtime.GOOS + "-" + arch
}

func prepareADPM(stage string, meta *metadata, platform string) error {
	if _, err := os.Stat(filepath.Join(stage, "META.json")); err == nil {
		return nil
	}
	payload := filepath.Join(stage, "payload", platform)
	if err := os.MkdirAll(payload, 0755); err != nil {
		return err
	}
	entries, err := os.ReadDir(stage)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.Name() == "payload" {
			continue
		}
		if err := os.Rename(filepath.Join(stage, e.Name()), filepath.Join(payload, e.Name())); err != nil {
			return err
		}
	}
	for _, pair := range [][2]string{{"usr/bin", "bin"}, {"usr/sbin", "bin"}, {"bin", "bin"}, {"sbin", "bin"}, {"usr/lib", "lib"}, {"usr/lib64", "lib"}, {"lib", "lib"}, {"lib64", "lib"}} {
		src := filepath.Join(payload, filepath.FromSlash(pair[0]))
		dst := filepath.Join(stage, pair[1], platform)
		if info, err := os.Stat(src); err == nil && info.IsDir() {
			if err := copyTreeFiles(src, dst); err != nil {
				return err
			}
		}
	}
	meta.Platforms = []string{platform}
	b, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(stage, "META.json"), b, 0644); err != nil {
		return err
	}
	install := "#!/bin/sh\n# Installation is managed by the adpm CLI.\nexit 0\n"
	return os.WriteFile(filepath.Join(stage, "INSTALL.sh"), []byte(install), 0755)
}

func copyTreeFiles(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			return err
		}
		_, err = io.Copy(out, in)
		closeErr := out.Close()
		if err == nil {
			err = closeErr
		}
		return err
	})
}

func prepareNative(stage, platform string) (string, error) {
	if _, err := os.Stat(filepath.Join(stage, "META.json")); err != nil {
		return stage, nil
	}
	native, err := os.MkdirTemp("", "adpm-native-")
	if err != nil {
		return "", err
	}
	payload := filepath.Join(stage, "payload", platform)
	if info, statErr := os.Stat(payload); statErr == nil && info.IsDir() {
		if err := copyTreeFiles(payload, native); err != nil {
			os.RemoveAll(native)
			return "", err
		}
	}
	for _, pair := range [][2]string{{"bin", "usr/bin"}, {"lib", "usr/lib"}} {
		src := filepath.Join(stage, pair[0], platform)
		if info, statErr := os.Stat(src); statErr == nil && info.IsDir() {
			if err := copyTreeFiles(src, filepath.Join(native, filepath.FromSlash(pair[1]))); err != nil {
				os.RemoveAll(native)
				return "", err
			}
		}
	}
	return native, nil
}

func outputPath(value, name, version, format string) (string, error) {
	ext := map[string]string{"tgz": ".tgz", "bpm": ".bpm", "adpm": ".adpm", "cpio": ".cpio", "cpio.gz": ".cpio.gz", "cpio.bz2": ".cpio.bz2", "rpm": ".rpm", "deb": ".deb"}[format]
	if value == "" {
		value = "."
	}
	abs, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	if info, err := os.Stat(abs); err == nil && info.IsDir() {
		return filepath.Join(abs, name+"-"+version+ext), nil
	}
	if filepath.Ext(abs) != "" {
		return abs, nil
	}
	return filepath.Join(abs, name+"-"+version+ext), nil
}

func emit(stage, output, format string, meta metadata, platform string) error {
	switch format {
	case "tgz":
		return pkgarchive.WriteTarGz(stage, output)
	case "bpm":
		return pkgarchive.WriteCPIO(stage, output, "gzip")
	case "adpm", "cpio.bz2":
		return pkgarchive.WriteCPIO(stage, output, "bzip2")
	case "cpio.gz":
		return pkgarchive.WriteCPIO(stage, output, "gzip")
	case "cpio":
		return pkgarchive.WriteCPIO(stage, output, "none")
	case "rpm", "deb":
		if _, err := exec.LookPath("fpm"); err != nil {
			return fmt.Errorf("fpm is required to create %s packages", format)
		}
		arch := strings.TrimPrefix(platform, "linux-")
		if format == "deb" {
			if arch == "x86_64" {
				arch = "amd64"
			}
			if arch == "aarch64" {
				arch = "arm64"
			}
		}
		args := []string{"-s", "dir", "-t", format, "-n", meta.Name, "-v", meta.Version, "-a", arch, "-C", stage, "-p", output, "."}
		cmd := exec.Command("fpm", args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}
	return fmt.Errorf("unsupported output %s", format)
}

func rpmMetadata(path string) (string, string, error) {
	cmd := exec.Command("rpm", "-qp", "--queryformat", "%{NAME}|%{VERSION}", path)
	out, err := cmd.Output()
	if err != nil {
		return "", "", err
	}
	p := strings.SplitN(string(out), "|", 2)
	if len(p) != 2 {
		return "", "", fmt.Errorf("invalid rpm metadata")
	}
	return p[0], p[1], nil
}
func debMetadata(path string) (string, string, error) {
	cmd := exec.Command("dpkg-deb", "-f", path, "Package", "Version")
	out, err := cmd.Output()
	if err != nil {
		return "", "", err
	}
	var n, v string
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "Package:") {
			n = strings.TrimSpace(strings.TrimPrefix(line, "Package:"))
		}
		if strings.HasPrefix(line, "Version:") {
			v = strings.TrimSpace(strings.TrimPrefix(line, "Version:"))
		}
	}
	if n == "" {
		fields := bytes.Fields(out)
		if len(fields) > 0 {
			n = string(fields[0])
		}
		if len(fields) > 1 {
			v = string(fields[1])
		}
	}
	return n, v, nil
}
