package pcx

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestEncoder(t *testing.T) {
	savePNG := os.Getenv("PCX_TEST_SAVE") != ""

	testImages, err := filepath.Glob("testdata/*.pcx")
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range testImages {
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		img, _, err := image.Decode(file)
		file.Close()
		if err != nil {
			t.Errorf("Failed to read header for %s: %s", path, err)
			continue
		}
		if err != nil {
			t.Errorf("Failed to decode %s: %s", path, err)
			continue
		}

		buf := &bytes.Buffer{}
		if err := Encode(buf, img); err != nil {
			t.Fatal(err)
		}
		if img, err := Decode(buf); err != nil {
			t.Fatal(err)
		} else if savePNG {
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
