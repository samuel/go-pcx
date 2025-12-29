package pcx

import (
	"bytes"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestDecoder(t *testing.T) {
	t.Parallel()
	savePNG := os.Getenv("PCX_TEST_SAVE") != ""

	testImages, err := filepath.Glob("testdata/*.pcx")
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range testImages {
		t.Run(filepath.Base(path), func(t *testing.T) {
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
			t.Logf("  %s : %dx%d version:%d pb4:%+v nplanes:%d bpp:%d", path, d.bounds.Dx(), d.bounds.Dy(), d.version, d.pb4, d.nplanes, d.bpp)
			img, err := d.decode()
			if err != nil {
				t.Fatalf("Failed to decode %s: %s", path, err)
				return
			}

			if savePNG {
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
