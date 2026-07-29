package pcx

import (
	"bytes"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecoder(t *testing.T) {
	t.Parallel()
	if *flagSave {
		if err := os.MkdirAll("testout", 0o755); err != nil {
			t.Fatal(err)
		}
	}

	testImages, err := filepath.Glob("testdata/*.pcx")
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range testImages {
		name := filepath.Base(path)
		if strings.HasPrefix(name, ".") {
			continue
		}
		t.Run(name, func(t *testing.T) {
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}

			cfg, err := DecodeConfig(bytes.NewReader(b))
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("%+#v", cfg)

			d, err := newDecoder(bytes.NewReader(b))
			if err != nil {
				t.Fatalf("Failed to read header for %s: %s", path, err)
				return
			}
			t.Logf("  %s : %dx%d version:%d palinfo:%d nplanes:%d bpp:%d", path, d.bounds.Dx(), d.bounds.Dy(), d.version, d.paletteInfo, d.nplanes, d.bpp)
			img, err := d.decode()
			if err != nil {
				t.Fatalf("Failed to decode %s: %s", path, err)
				return
			}

			if *flagSave {
				w, err := os.Create(fmt.Sprintf("testout/out-%s.png", filepath.Base(path)))
				if err != nil {
					t.Fatal(err)
				}
				err = png.Encode(w, img)
				if err := w.Close(); err != nil {
					t.Log(err)
				}
				if err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
