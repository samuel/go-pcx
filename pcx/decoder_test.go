package pcx

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestDecoder(t *testing.T) {
	savePNG := os.Getenv("PCX_TEST_SAVE") != ""

	testImages, err := filepath.Glob("testfiles/*.pcx")
	if err != nil {
		t.Fatal(err)
	}

	for _, filename := range testImages {
		file, err := os.Open(filename)
		if err != nil {
			t.Fatal(err)
		}
		d, err := newDecoder(file)
		if err != nil {
			t.Errorf("Failed to read header for %s: %s", filename, err.Error())
			file.Close()
			continue
		}
		t.Logf("%s : %dx%d version:%d pb4:%+v nplanes:%d bpp:%d", filename, d.bounds.Dx(), d.bounds.Dy(), d.version, d.pb4, d.nplanes, d.bpp)
		img, err := d.decode()
		file.Close()
		if err != nil {
			t.Errorf("Failed to decode %s: %s", filename, err.Error())
			continue
		}

		if savePNG {
			w, err := os.Create(fmt.Sprintf("out-%s.png", filename))
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
