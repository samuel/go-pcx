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
	savePNG := os.Getenv("PCX_TEST_SAVE") != ""

	testImages, err := filepath.Glob("testdata/*.pcx")
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range testImages {
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
			t.Errorf("Failed to read header for %s: %s", path, err)
			continue
		}
		t.Logf("%s : %dx%d version:%d pb4:%+v nplanes:%d bpp:%d", path, d.bounds.Dx(), d.bounds.Dy(), d.version, d.pb4, d.nplanes, d.bpp)
		img, err := d.decode()
		if err != nil {
			t.Errorf("Failed to decode %s: %s", path, err)
			continue
		}

		if savePNG {
			w, err := os.Create(fmt.Sprintf("testout/out-%s.png", filepath.Base(path)))
			if err != nil {
				t.Fatal(err)
			}
			err = png.Encode(w, img)
			w.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
	}
}
