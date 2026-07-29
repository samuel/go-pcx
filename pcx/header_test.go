package pcx

import (
	"bytes"
	"image"
	"image/color"
	"testing"
)

// u16 reads a little-endian 16-bit header field.
func u16(hdr []byte, off int) int {
	return int(hdr[off]) | int(hdr[off+1])<<8
}

// fillOpaque sets every pixel of m to an opaque color.
func fillOpaque(m interface {
	image.Image
	Set(x, y int, c color.Color)
}) {
	b := m.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			m.Set(x, y, color.RGBA{R: byte(x * 8), G: byte(y * 8), B: 0x40, A: 0xff})
		}
	}
}

// encodeHeader encodes m and returns the 128 byte header it wrote. It also checks
// the parts of the header that hold for every variant, so the table below only has
// to name what differs between them.
func encodeHeader(t *testing.T, m image.Image) []byte {
	t.Helper()
	buf := &bytes.Buffer{}
	if err := Encode(buf, m); err != nil {
		t.Fatalf("Encode: %s", err)
	}
	if buf.Len() < 128 {
		t.Fatalf("wrote %d bytes, less than a 128 byte header", buf.Len())
	}
	hdr := buf.Bytes()[:128]
	if hdr[0] != magic {
		t.Errorf("header byte 0 is %#02x, expected the %#02x magic", hdr[0], magic)
	}
	if hdr[2] != 1 {
		t.Errorf("header byte 2 is %d, expected 1 (RLE); the decoder rejects anything else", hdr[2])
	}
	// Bytes 64, 69 and everything from 74 on are reserved and must be zero. The
	// decoder ignores them, but a real PCX reader may not.
	if hdr[64] != 0 || hdr[69] != 0 {
		t.Errorf("reserved bytes 64/69 are %d/%d, expected 0/0", hdr[64], hdr[69])
	}
	for i := 74; i < 128; i++ {
		if hdr[i] != 0 {
			t.Errorf("reserved header byte %d is %d, expected 0", i, hdr[i])
		}
	}
	// bytesPerLine is per plane and the format requires it to be even.
	if bpl := u16(hdr, 66); bpl&1 != 0 {
		t.Errorf("bytes per line (%d) is odd", bpl)
	}
	return hdr
}

// TestEncodeHeader pins the header each encode path writes, over synthetic images
// whose expected variant is known. It replaces the byte-for-byte header comparison
// TestEncoder used to attempt against its fixtures, which could only ever assert
// that the encoder agreed with whichever writer produced the fixture.
func TestEncodeHeader(t *testing.T) {
	t.Parallel()

	bounds := image.Rect(0, 0, 5, 3)
	// A fresh RGBA/NRGBA is transparent black, so the opaque cases have to be filled:
	// the plane count is chosen from the alpha, not the type.
	opaqueRGBA := image.NewRGBA(bounds)
	fillOpaque(opaqueRGBA)
	translucentRGBA := image.NewRGBA(bounds)
	fillOpaque(translucentRGBA)
	translucentRGBA.SetRGBA(1, 1, color.RGBA{R: 40, G: 30, B: 20, A: 128})
	opaqueNRGBA := image.NewNRGBA(bounds)
	fillOpaque(opaqueNRGBA)
	translucentNRGBA := image.NewNRGBA(bounds)
	fillOpaque(translucentNRGBA)
	translucentNRGBA.SetNRGBA(1, 1, color.NRGBA{R: 200, G: 100, B: 50, A: 128})

	paletted := func(pal color.Palette, w int) *image.Paletted {
		return image.NewPaletted(image.Rect(0, 0, w, 3), pal)
	}

	for _, tc := range []struct {
		name         string
		img          image.Image
		bpp, nplanes int
		bytesPerLine int
		paletteInfo  int
	}{
		// Truecolor: three planes while the image is opaque, four once it isn't, and
		// the odd width is padded to an even bytes per line.
		{"opaque RGBA", opaqueRGBA, 8, 3, 6, paletteInfoColor},
		{"translucent RGBA", translucentRGBA, 8, 4, 6, paletteInfoColor},
		{"opaque NRGBA", opaqueNRGBA, 8, 3, 6, paletteInfoColor},
		{"translucent NRGBA", translucentNRGBA, 8, 4, 6, paletteInfoColor},
		// Grayscale is the only path that writes 2 into byte 68; that plus the absent
		// extended palette is what the decoder reads back as *image.Gray.
		{"gray", image.NewGray(bounds), 8, 1, 6, paletteInfoGrayscale},
		// 8bpp paletted: one byte per pixel, palette after the pixel data.
		{"256 colors", paletted(grayRampPalette(256), 5), 8, 1, 6, paletteInfoColor},
		// Sub-8bpp: bytes per line follows from the pixels packed per byte, rounded
		// up to even. 17 pixels need 3 bytes at 1bpp and 5 at 2bpp.
		{"black and white", paletted(color.Palette{color.Black, color.White}, 17), 1, 1, 4, paletteInfoColor},
		{"4 colors", paletted(grayRampPalette(4), 17), 2, 1, 6, paletteInfoColor},
		{"8 colors", paletted(grayRampPalette(8), 24), 1, 3, 4, paletteInfoColor},
		{"16 colors", paletted(grayRampPalette(16), 24), 1, 4, 4, paletteInfoColor},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			hdr := encodeHeader(t, tc.img)
			if got := int(hdr[3]); got != tc.bpp {
				t.Errorf("bpp is %d, expected %d", got, tc.bpp)
			}
			if got := int(hdr[65]); got != tc.nplanes {
				t.Errorf("nplanes is %d, expected %d", got, tc.nplanes)
			}
			if got := u16(hdr, 66); got != tc.bytesPerLine {
				t.Errorf("bytes per line is %d, expected %d", got, tc.bytesPerLine)
			}
			if got := int(hdr[68]); got != tc.paletteInfo {
				t.Errorf("palette info is %d, expected %d", got, tc.paletteInfo)
			}
			// Without a *pcx.ImageWithHeader to steer it, the encoder claims version 5.
			if hdr[1] != 5 {
				t.Errorf("version is %d, expected 5", hdr[1])
			}
		})
	}
}

// TestEncodeHeaderBounds checks that the origin and the inclusive maximum are
// written as the format stores them, for an image that does not start at (0,0).
func TestEncodeHeaderBounds(t *testing.T) {
	t.Parallel()

	m := image.NewRGBA(image.Rect(3, 4, 8, 7))
	hdr := encodeHeader(t, m)
	for _, f := range []struct {
		label string
		off   int
		want  int
	}{
		{"xmin", 4, 3},
		{"ymin", 6, 4},
		// The maximum is inclusive in the file and exclusive in image.Rectangle.
		{"xmax", 8, 7},
		{"ymax", 10, 6},
	} {
		if got := u16(hdr, f.off); got != f.want {
			t.Errorf("%s is %d, expected %d", f.label, got, f.want)
		}
	}
	// The dimensions have to survive to the decoder as the original bounds.
	if cfg, err := DecodeConfig(bytes.NewReader(hdr)); err != nil {
		t.Fatal(err)
	} else if cfg.Width != 5 || cfg.Height != 3 {
		t.Errorf("decoded config is %dx%d, expected 5x3", cfg.Width, cfg.Height)
	}
}

// TestEncodeHeaderMetadata checks that the header fields only a *pcx.ImageWithHeader can steer
// are written, and that DecodeExtended reads them back.
func TestEncodeHeaderMetadata(t *testing.T) {
	t.Parallel()

	in := &ImageWithHeader{
		Image:           image.NewRGBA(image.Rect(0, 0, 4, 2)),
		Version:         3,
		PaletteInfoSet:  true,
		HorizDPI:        300,
		VertDPI:         301,
		HorizScreenSize: 640,
		VertScreenSize:  480,
	}
	buf := &bytes.Buffer{}
	if err := Encode(buf, in); err != nil {
		t.Fatalf("Encode: %s", err)
	}
	hdr := bytes.Clone(buf.Bytes()[:128])
	if hdr[1] != 3 {
		t.Errorf("version is %d, expected 3", hdr[1])
	}
	for _, f := range []struct {
		label string
		off   int
		want  int
	}{
		{"horizontal DPI", 12, 300},
		{"vertical DPI", 14, 301},
		{"horizontal screen size", 70, 640},
		{"vertical screen size", 72, 480},
	} {
		if got := u16(hdr, f.off); got != f.want {
			t.Errorf("%s is %d, expected %d", f.label, got, f.want)
		}
	}

	out, err := DecodeExtended(buf)
	if err != nil {
		t.Fatalf("DecodeExtended: %s", err)
	}
	if out.Version != in.Version || out.HorizDPI != in.HorizDPI || out.VertDPI != in.VertDPI ||
		out.HorizScreenSize != in.HorizScreenSize || out.VertScreenSize != in.VertScreenSize {
		t.Errorf("metadata round trip changed %+v into %+v", in, out)
	}
	if !out.PaletteInfoSet {
		t.Error("PaletteInfoSet did not survive the round trip")
	}
}
