package pcx

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"os"
	"testing"
)

// grayReference loads the PNG the grayscale fixtures were generated from. It is the
// ground truth for their pixel content, produced by a different toolchain entirely.
func grayReference(t *testing.T) *image.Gray {
	t.Helper()
	f, err := os.Open("testdata/gray8.png")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	}()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	ref, ok := img.(*image.Gray)
	if !ok {
		t.Fatalf("reference image is %T, expected *image.Gray", img)
	}
	return ref
}

// checkAgainstReference verifies img has the same size and gray levels as ref.
func checkAgainstReference(t *testing.T, img image.Image, ref *image.Gray) {
	t.Helper()
	if img.Bounds() != ref.Bounds() {
		t.Fatalf("bounds are %v, expected %v", img.Bounds(), ref.Bounds())
	}
	b := ref.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, a := img.At(x, y).RGBA()
			want := uint32(ref.GrayAt(x, y).Y) * 0x101
			if r != g || g != bl {
				t.Fatalf("pixel (%d,%d) is %v, expected a gray level", x, y, img.At(x, y))
			}
			if r != want || a != 0xffff {
				t.Fatalf("pixel (%d,%d) is %d (alpha %d), expected %d (alpha 65535)", x, y, r, a, want)
			}
		}
	}
}

// TestDecodeGrayscale covers both forms an 8bpp single plane grayscale file takes.
// See testdata/gray-pcx.txt for how the fixtures were produced.
func TestDecodeGrayscale(t *testing.T) {
	t.Parallel()
	ref := grayReference(t)

	tests := []struct {
		name string
		path string
		// The concrete type Decode is expected to return.
		paletted bool
	}{
		// The form every PCX writer actually emits: a real grayscale ramp in the
		// extended palette, which makes it an ordinary paletted image.
		{name: "extended palette", path: "testdata/gray8.pcx", paletted: true},
		// The form the spec describes but no writer emits: no palette at all, with
		// the palette info byte marking the image grayscale.
		{name: "palette info", path: "testdata/gray8_nopal.pcx"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			b, err := os.ReadFile(test.path)
			if err != nil {
				t.Fatal(err)
			}
			img, err := Decode(bytes.NewReader(b))
			if err != nil {
				t.Fatalf("failed to decode %s: %s", test.path, err)
			}
			if test.paletted {
				pal, ok := img.(*image.Paletted)
				if !ok {
					t.Fatalf("expected *image.Paletted, got %T", img)
				}
				if len(pal.Palette) != 256 {
					t.Errorf("expected a 256 color palette, got %d colors", len(pal.Palette))
				}
			} else if _, ok := img.(*image.Gray); !ok {
				t.Fatalf("expected *image.Gray, got %T", img)
			}
			checkAgainstReference(t, img, ref)

			// The same pixels must survive a round trip through the encoder, and the
			// image type must be stable across it.
			buf := &bytes.Buffer{}
			if err := Encode(buf, img); err != nil {
				t.Fatalf("failed to encode: %s", err)
			}
			img2, err := Decode(buf)
			if err != nil {
				t.Fatalf("failed to decode after encode: %s", err)
			}
			if got, want := fmt.Sprintf("%T", img2), fmt.Sprintf("%T", img); got != want {
				t.Errorf("round trip changed the image type from %s to %s", want, got)
			}
			checkAgainstReference(t, img2, ref)
		})
	}
}
