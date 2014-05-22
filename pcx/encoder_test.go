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

	testImages, err := filepath.Glob("testfiles/*.pcx")
	if err != nil {
		t.Fatal(err)
	}

	for _, filename := range testImages {
		file, err := os.Open(filename)
		if err != nil {
			t.Fatal(err)
		}
		img, _, err := image.Decode(file)
		file.Close()
		if err != nil {
			t.Errorf("Failed to read header for %s: %s", filename, err.Error())
			continue
		}
		if err != nil {
			t.Errorf("Failed to decode %s: %s", filename, err.Error())
			continue
		}

		buf := &bytes.Buffer{}
		if err := Encode(buf, img); err != nil {
			t.Fatal(err)
		}
		if img, err := Decode(buf); err != nil {
			t.Fatal(err)
		} else if savePNG {
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
