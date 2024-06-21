package pcx

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func FuzzDecodeEncode(f *testing.F) {
	testImages, err := filepath.Glob("testdata/*.pcx")
	if err != nil {
		f.Fatal(err)
	}
	for _, path := range testImages {
		b, err := os.ReadFile(path)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		d, err := newDecoder(bytes.NewReader(data))
		if err != nil {
			return
		}
		if d.bounds.Dx()*d.bounds.Dy() > 20e6 {
			return
		}

		img, err := d.decode()
		if err != nil {
			return
		}
		var w bytes.Buffer
		if err := Encode(&w, img); err != nil {
			t.Fatalf("Encode failed: %s", err)
		}
		img1, err := Decode(&w)
		if err != nil {
			t.Fatalf("Decode of encode failed: %s", err)
		}
		if !reflect.DeepEqual(img.Bounds(), img1.Bounds()) {
			t.Fatalf("Bounds of decoded image %#v don't match encode iamge %#v\n",
				img.Bounds(), img1.Bounds())
		}
	})
}
