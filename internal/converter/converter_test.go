package converter

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/afterdarksys/adpm/internal/pkgarchive"
)

func conversionFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	bin := filepath.Join(root, "usr", "bin")
	if err := os.MkdirAll(bin, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "fixture"), []byte("fixture\n"), 0755); err != nil {
		t.Fatal(err)
	}
	tgz := filepath.Join(t.TempDir(), "fixture.tgz")
	if err := pkgarchive.WriteTarGz(root, tgz); err != nil {
		t.Fatal(err)
	}
	return root, tgz
}

func TestFormatAliases(t *testing.T) {
	tests := map[string]string{"tar.gz": "tgz", "tgz": "tgz", "cpio.gzip": "cpio.gz", "cpio.bzip2": "cpio.bz2", "rpm": "rpm", "deb": "deb", "bpm": "bpm"}
	for input, want := range tests {
		t.Run(input, func(t *testing.T) {
			got, err := normalize(input)
			if err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Fatalf("normalize(%q)=%q, want %q", input, got, want)
			}
		})
	}
}

func TestPortableConversionMatrix(t *testing.T) {
	_, source := conversionFixture(t)
	formats := []string{"tgz", "bpm", "cpio", "cpio.gz", "cpio.bz2", "adpm"}
	for _, format := range formats {
		t.Run(format, func(t *testing.T) {
			if format == "cpio.bz2" || format == "adpm" {
				if _, err := exec.LookPath("bzip2"); err != nil {
					t.Skip("bzip2 unavailable")
				}
			}
			first := filepath.Join(t.TempDir(), "first"+map[string]string{"tgz": ".tgz", "bpm": ".bpm", "cpio": ".cpio", "cpio.gz": ".cpio.gz", "cpio.bz2": ".cpio.bz2", "adpm": ".adpm"}[format])
			err := Convert(ConversionOptions{InPkg: "tgz", Input: source, OutPkg: format, Output: first, Name: "matrix", Version: "1.0.0", Architecture: "linux-x86_64"})
			if err != nil {
				t.Fatal(err)
			}
			final := filepath.Join(t.TempDir(), "final.adpm")
			err = Convert(ConversionOptions{InPkg: format, Input: first, OutPkg: "adpm", Output: final, Name: "matrix", Version: "1.0.0", Architecture: "linux-x86_64"})
			if err != nil {
				t.Fatal(err)
			}
			extracted := t.TempDir()
			if err := pkgarchive.ExtractAuto(final, extracted); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(extracted, "META.json")); err != nil {
				t.Fatal("converted ADPM missing META.json")
			}
			metadataBytes, err := os.ReadFile(filepath.Join(extracted, "META.json"))
			if err != nil {
				t.Fatal(err)
			}
			var metadata map[string]interface{}
			if err := json.Unmarshal(metadataBytes, &metadata); err != nil {
				t.Fatal(err)
			}
			provenance, ok := metadata["provenance"].(map[string]interface{})
			if !ok || provenance["method"] != "adpm-convert" || provenance["source_sha256"] == "" {
				t.Fatalf("converted ADPM missing package provenance: %#v", metadata["provenance"])
			}
			payload := filepath.Join(extracted, "payload", "linux-x86_64", "usr", "bin", "fixture")
			if format == "adpm" {
				payload = filepath.Join(extracted, "payload", "linux-x86_64", "usr", "bin", "fixture")
			}
			if _, err := os.Stat(payload); err != nil {
				t.Fatalf("converted payload missing: %v", err)
			}
		})
	}
}

func TestNativeOutputFormatsInvokeFPM(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	_, source := conversionFixture(t)
	tools := t.TempDir()
	log := filepath.Join(t.TempDir(), "fpm.log")
	script := "#!/bin/sh\nout=\"\"\nwhile [ $# -gt 0 ]; do if [ \"$1\" = -p ]; then out=$2; shift 2; else shift; fi; done\nprintf called > \"$FPM_LOG\"\n: > \"$out\"\n"
	if err := os.WriteFile(filepath.Join(tools, "fpm"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tools+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FPM_LOG", log)
	for _, format := range []string{"rpm", "deb"} {
		t.Run(format, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "fixture."+format)
			if err := Convert(ConversionOptions{InPkg: "tgz", Input: source, OutPkg: format, Output: out, Name: "fixture", Version: "1.0.0", Architecture: "linux-x86_64"}); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(out); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func writeTool(t *testing.T, directory, name, script string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(directory, name), []byte("#!/bin/sh\n"+script), 0755); err != nil {
		t.Fatal(err)
	}
}

func TestRPMInputUsesRPMMetadataAndCPIOPayload(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	fixture, _ := conversionFixture(t)
	payload := filepath.Join(t.TempDir(), "payload.cpio")
	if err := pkgarchive.WriteCPIO(fixture, payload, "none"); err != nil {
		t.Fatal(err)
	}
	tools := t.TempDir()
	writeTool(t, tools, "rpm", "printf 'rpm-fixture|2.3.4'")
	writeTool(t, tools, "rpm2cpio", "cat \"$RPM_PAYLOAD\"")
	t.Setenv("RPM_PAYLOAD", payload)
	t.Setenv("PATH", tools+string(os.PathListSeparator)+os.Getenv("PATH"))
	input := filepath.Join(t.TempDir(), "fixture.rpm")
	if err := os.WriteFile(input, []byte("rpm"), 0644); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "fixture.tgz")
	if err := Convert(ConversionOptions{InPkg: "rpm", Input: input, OutPkg: "tgz", Output: output}); err != nil {
		t.Fatal(err)
	}
	extracted := t.TempDir()
	if err := pkgarchive.ExtractAuto(output, extracted); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(extracted, "usr", "bin", "fixture")); err != nil {
		t.Fatal(err)
	}
}

func TestDEBInputUsesMetadataAndExtractedPayload(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	fixture, _ := conversionFixture(t)
	tools := t.TempDir()
	script := `if [ "$1" = "-f" ]; then
  printf 'Package: deb-fixture\nVersion: 4.5.6\n'
elif [ "$1" = "-x" ]; then
  cp -R "$DEB_PAYLOAD/." "$3"
else
  exit 1
fi
`
	writeTool(t, tools, "dpkg-deb", script)
	t.Setenv("DEB_PAYLOAD", fixture)
	t.Setenv("PATH", tools+string(os.PathListSeparator)+os.Getenv("PATH"))
	input := filepath.Join(t.TempDir(), "fixture.deb")
	if err := os.WriteFile(input, []byte("deb"), 0644); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "fixture.tgz")
	if err := Convert(ConversionOptions{InPkg: "deb", Input: input, OutPkg: "tgz", Output: output}); err != nil {
		t.Fatal(err)
	}
	extracted := t.TempDir()
	if err := pkgarchive.ExtractAuto(output, extracted); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(extracted, "usr", "bin", "fixture")); err != nil {
		t.Fatal(err)
	}
}
