package pcx

import (
	"bytes"
	"image"
	"image/color"
	"testing"
)

// roundTripPaletted encodes a w x h image with the given palette, decodes it again and
// checks that the pixel indices and the palette colors both survived. It returns the
// decoded image and the encoded bytes so a caller can also assert which variant was
// chosen. The palette length is deliberately not checked: every sub-8bpp variant
// decodes to 1<<bpp or 1<<nplanes entries, so a palette can only round up.
func roundTripPaletted(t *testing.T, pal color.Palette, w, h int) (*image.Paletted, []byte) {
	t.Helper()
	in := image.NewPaletted(image.Rect(0, 0, w, h), pal)
	for i := range in.Pix {
		in.Pix[i] = byte(i % len(pal))
	}

	buf := &bytes.Buffer{}
	if err := Encode(buf, in); err != nil {
		t.Fatalf("Encode: %s", err)
	}
	encoded := bytes.Clone(buf.Bytes())

	decoded, err := Decode(buf)
	if err != nil {
		t.Fatalf("Decode: %s", err)
	}
	out, ok := decoded.(*image.Paletted)
	if !ok {
		t.Fatalf("Expected *image.Paletted, got %T", decoded)
	}
	if !bytes.Equal(in.Pix, out.Pix) {
		t.Errorf("Pix differ:\n in %v\nout %v", in.Pix, out.Pix)
	}
	if len(out.Palette) < len(pal) {
		t.Fatalf("Expected at least %d palette entries, got %d", len(pal), len(out.Palette))
	}
	for i, want := range pal {
		wr, wg, wb, _ := want.RGBA()
		gr, gg, gb, _ := out.Palette[i].RGBA()
		if wr != gr || wg != gg || wb != gb {
			t.Errorf("Palette entry %d is %v, expected %v", i, out.Palette[i], want)
		}
	}
	return out, encoded
}

// TestEncodePalettePreservation covers the promise that a small palette keeps its
// colors through an encode, whichever sub-8bpp variant the encoder picks for it.
func TestEncodePalettePreservation(t *testing.T) {
	t.Parallel()
	var (
		red  = color.RGBA{R: 0xff, A: 0xff}
		blue = color.RGBA{B: 0xff, A: 0xff}
		dark = color.RGBA{R: 10, G: 20, B: 30, A: 0xff}
		gray = color.RGBA{R: 40, G: 50, B: 60, A: 0xff}
	)

	for _, tc := range []struct {
		name         string
		pal          color.Palette
		w, h         int
		bpp, nplanes byte
	}{
		// A two color palette that isn't black and white can't use the 1bpp variant,
		// which stores no colors at all.
		{"2 colors", color.Palette{red, blue}, 17, 3, 2, 1},
		{"black and white", color.Palette{color.Black, color.White}, 17, 3, 1, 1},
		{"1 color", color.Palette{red}, 5, 2, 2, 1},
		{"3 colors", color.Palette{dark, gray, red}, 9, 2, 2, 1},
		{"4 colors", color.Palette{dark, gray, red, blue}, 321, 200, 2, 1},
		// The decoder rebuilds the palette of any 2bpp 320x200 file from the CGA
		// tables, so a palette that isn't CGA has to take the 8bpp form there.
		{"non-CGA at 320x200", color.Palette{dark, gray, red, blue}, 320, 200, 8, 1},
		{"2 colors at 320x200", color.Palette{red, blue}, 320, 200, 8, 1},
		// A real CGA palette still gets the compact 2bpp form. Palette 2 (cyan,
		// magenta, light gray); the color burst palettes can't be re-encoded.
		{"CGA at 320x200", color.Palette{
			cga16ColorPalette[0], cga16ColorPalette[3], cga16ColorPalette[5], cga16ColorPalette[7],
		}, 320, 200, 2, 1},
		{"8 colors", grayRampPalette(8), 24, 3, 1, 3},
		{"9 colors", grayRampPalette(9), 24, 3, 1, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, encoded := roundTripPaletted(t, tc.pal, tc.w, tc.h)
			if encoded[3] != tc.bpp || encoded[65] != tc.nplanes {
				t.Errorf("Encoded as %d bpp / %d planes, expected %d / %d",
					encoded[3], encoded[65], tc.bpp, tc.nplanes)
			}
		})
	}
}

// TestEncodePalettedBytesPerLineEven pins the format's requirement that bytes per line
// be even for the planar variant, whose widths otherwise produce an odd count.
func TestEncodePalettedBytesPerLineEven(t *testing.T) {
	t.Parallel()
	for _, n := range []int{5, 8, 9, 16} {
		for _, w := range []int{7, 21, 24, 88} {
			_, encoded := roundTripPaletted(t, grayRampPalette(n), w, 3)
			if bpl := int(encoded[66]) | int(encoded[67])<<8; bpl&1 != 0 {
				t.Errorf("%d colors at width %d: odd bytes per line (%d)", n, w, bpl)
			}
		}
	}
}

func grayRampPalette(n int) color.Palette {
	pal := make(color.Palette, n)
	for i := range pal {
		v := byte(i * 255 / (n - 1))
		pal[i] = color.RGBA{R: v, G: v, B: v, A: 0xff}
	}
	return pal
}
