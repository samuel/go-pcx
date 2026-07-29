package pcx

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEncoder(t *testing.T) {
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
			inBytes, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			img, err := DecodeExtended(bytes.NewReader(inBytes))
			if err != nil {
				t.Errorf("Failed to decode %s: %s", path, err)
				return
			}
			m, _ := newDecoder(bytes.NewReader(inBytes))
			t.Logf("Input: %+#v", m)

			buf := &bytes.Buffer{}
			if err := Encode(buf, img); err != nil {
				t.Fatal(err)
			}

			outBytes := buf.Bytes()
			// Ignore some unused values in the comparison
			inBytes[64] = 0
			inBytes[68] = outBytes[68] & 3
			inBytes[69] = 0
			for i := 74; i < 128; i++ {
				inBytes[i] = 0
			}
			if inBytes[3] == 8 && inBytes[65] == 1 {
				// if bpp == 8 and nplanes == 1 then ignore the EGA palette
				for i := 16; i < 16+48; i++ {
					inBytes[i] = outBytes[i]
				}
			} else if inBytes[3] == 2 && inBytes[65] == 1 {
				// Ignore unused colors
				for i := 16 + 3*4; i < 16+48; i++ {
					inBytes[i] = outBytes[i]
				}
			}

			// Make sure we can decode the new image.
			img2, err := Decode(buf)
			if err != nil {
				m, _ := newDecoder(bytes.NewReader(inBytes))
				t.Logf("Output: %+#v", m)
				t.Fatalf("Failed to decode after encode: %s", err)
			}

			if *flagSave {
				w, err := os.Create(fmt.Sprintf("testout/out-%s-0.png", filepath.Base(path)))
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

				w, err = os.Create(fmt.Sprintf("testout/out-%s-1.png", filepath.Base(path)))
				if err != nil {
					t.Fatal(err)
				}
				err = png.Encode(w, img2)
				if err := w.Close(); err != nil {
					t.Log(err)
				}
				if err != nil {
					t.Fatal(err)
				}
			}

			switch imgIn := img.Image.(type) {
			case *image.RGBA:
				imgOut, ok := img2.(*image.RGBA)
				if !ok {
					t.Fatalf("Expected *image.RGBA, got %T", img2)
				} else if !bytes.Equal(imgIn.Pix, imgOut.Pix) {
					t.Fatalf("Output image of type *image.RGBA differs")
				}
			case *image.NRGBA:
				imgOut, ok := img2.(*image.NRGBA)
				if !ok {
					t.Fatalf("Expected *image.NRGBA, got %T", img2)
				} else if !bytes.Equal(imgIn.Pix, imgOut.Pix) {
					t.Fatalf("Output image of type *image.NRGBA differs")
				}
			case *image.Gray:
				imgOut, ok := img2.(*image.Gray)
				if !ok {
					t.Fatalf("Expected *image.Gray, got %T", img2)
				} else if !bytes.Equal(imgIn.Pix, imgOut.Pix) {
					t.Fatalf("Output image of type *image.Gray differs")
				}
			case *image.Paletted:
				imgOut, ok := img2.(*image.Paletted)
				if !ok {
					t.Fatalf("Expected *image.Paletted, got %T", img2)
				}
				// Palette length can't round-trip exactly: every sub-8bpp variant
				// decodes to 1<<bpp or 1<<nplanes entries, so a palette only ever
				// rounds up to the next representable size. The colors themselves
				// must survive.
				if len(imgOut.Palette) < len(imgIn.Palette) {
					t.Fatalf("Expected output image palette with at least %d colors, got %d",
						len(imgIn.Palette), len(imgOut.Palette))
				}
				for i, exp := range imgIn.Palette {
					if c := imgOut.Palette[i]; c != exp {
						t.Fatalf("Output image palette entry %d is %v, expected %v", i, c, exp)
					}
				}
				if !bytes.Equal(imgIn.Pix, imgOut.Pix) {
					t.Fatalf("Output image of type *image.Paletted differs")
				}
			default:
				t.Fatalf("Unhandled image type %T", img.Image)
			}
		})
	}
}
