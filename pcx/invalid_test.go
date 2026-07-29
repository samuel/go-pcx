package pcx

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"io"
	"os"
	"testing"
)

// fixtureBytes reads a fixture and returns a copy safe to mutate.
func fixtureBytes(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.Clone(b)
}

// put16 writes a little-endian 16-bit header field.
func put16(b []byte, off, v int) {
	b[off] = byte(v)
	b[off+1] = byte(v >> 8)
}

// TestDecodeMalformedHeader covers the header-level rejections, each of which was
// once either a garbage image, an unbounded allocation or an outright panic. The
// mutations are applied to a real fixture so that everything except the field under
// test stays valid.
func TestDecodeMalformedHeader(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		// mutate corrupts a copy of vball.pcx, an 8bpp single plane file.
		mutate func(b []byte) []byte
		// header is true when the failure must come from the header read, so that
		// DecodeConfig rejects the file too and no pixel buffer is ever allocated.
		header bool
		want   error
	}{{
		// nplanes 0 also defeated the geometry check, since it made both sides of
		// that comparison 0.
		name:   "no planes",
		mutate: func(b []byte) []byte { b[65] = 0; return b },
		header: true,
		want:   FormatError(""),
	}, {
		// A bpp the format never defines used to run the pixel loop with a byte
		// shift that underflowed, producing garbage rather than an error.
		name:   "bpp 3",
		mutate: func(b []byte) []byte { b[3] = 3; return b },
		header: true,
		want:   FormatError(""),
	}, {
		// 65535x65535 fits in the 16-bit header fields but is 4GB as a single plane.
		name: "over the pixel ceiling",
		mutate: func(b []byte) []byte {
			put16(b, 8, 0xfffe)
			put16(b, 10, 0xfffe)
			put16(b, 66, 0xfffe)
			return b
		},
		header: true,
		want:   FormatError(""),
	}, {
		// An inclusive maximum one below the origin is an empty image.
		name: "zero width",
		mutate: func(b []byte) []byte {
			put16(b, 4, 1)
			put16(b, 8, 0)
			return b
		},
		header: true,
		want:   FormatError(""),
	}, {
		// Too few bytes per line to hold a row of pixels.
		name:   "bytes per line too small",
		mutate: func(b []byte) []byte { put16(b, 66, 2); return b },
		header: true,
		want:   FormatError(""),
	}, {
		// A plane count that is valid in the format but unimplemented here has to
		// stay an UnsupportedError rather than becoming a FormatError.
		name:   "five planes",
		mutate: func(b []byte) []byte { b[65] = 5; return b },
		want:   UnsupportedError(""),
	}, {
		// A body that stops mid-scanline is truncated, never a clean end of input.
		name:   "truncated body",
		mutate: func(b []byte) []byte { return b[:200] },
		want:   io.ErrUnexpectedEOF,
	}, {
		// An 8bpp single plane file with neither a trailing palette nor the
		// grayscale marker has no colors at all.
		name:   "missing extended palette",
		mutate: func(b []byte) []byte { return b[:len(b)-769] },
		want:   FormatError(""),
	}, {
		// The same file marked grayscale is not an error: the pixel value is the
		// intensity. This is the tripwire for the check above being too eager.
		name: "missing extended palette but grayscale",
		mutate: func(b []byte) []byte {
			b[68] = paletteInfoGrayscale
			return b[:len(b)-769]
		},
		want: nil,
	}, {
		// A palette that is present but cut short stays a hard error even when the
		// grayscale marker would otherwise excuse an absent one.
		name: "truncated extended palette",
		mutate: func(b []byte) []byte {
			b[68] = paletteInfoGrayscale
			return b[:len(b)-8]
		},
		want: io.ErrUnexpectedEOF,
	}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			b := tc.mutate(fixtureBytes(t, "vball.pcx"))

			img, err := Decode(bytes.NewReader(b))
			checkErrorKind(t, "Decode", err, tc.want)
			if tc.want != nil && img != nil {
				// A failed decode must not hand back a partially filled image.
				t.Errorf("Decode returned a %T alongside its error", img)
			}
			if !tc.header {
				return
			}
			// The header read has to reject it too, both so DecodeConfig never
			// describes a file Decode refuses and so that the dimensions in a
			// hostile header are never used to allocate anything.
			_, err = DecodeConfig(bytes.NewReader(b))
			checkErrorKind(t, "DecodeConfig", err, tc.want)
		})
	}
}

// checkErrorKind compares err against want by kind: the typed errors by type, since
// their messages are not part of the contract, and everything else with errors.Is.
func checkErrorKind(t *testing.T, what string, err, want error) {
	t.Helper()
	switch want := want.(type) {
	case nil:
		if err != nil {
			t.Errorf("%s: %s", what, err)
		}
	case FormatError:
		var got FormatError
		if !errors.As(err, &got) {
			t.Errorf("%s: err is %v (%T), expected a FormatError", what, err, err)
		}
	case UnsupportedError:
		var got UnsupportedError
		if !errors.As(err, &got) {
			t.Errorf("%s: err is %v (%T), expected an UnsupportedError", what, err, err)
		}
	default:
		if !errors.Is(err, want) {
			t.Errorf("%s: err is %v (%T), expected %v", what, err, err, want)
		}
	}
}

// TestDecodeHugeHeaderAllocatesNothing pins that the pixel-count ceiling is enforced
// by the header read. A 65535x65535 header is only 128 bytes, so if it reached
// decode() it would ask for a 4GB pixel buffer; failing in newDecoder means no such
// buffer can exist, which is a stronger statement than any heap measurement taken
// while the rest of the suite runs in parallel.
func TestDecodeHugeHeaderAllocatesNothing(t *testing.T) {
	t.Parallel()

	b := fixtureBytes(t, "vball.pcx")[:128]
	put16(b, 4, 0)
	put16(b, 6, 0)
	put16(b, 8, 0xfffe)
	put16(b, 10, 0xfffe)
	put16(b, 66, 0xfffe)

	if _, err := newDecoder(bytes.NewReader(b)); err == nil {
		t.Fatal("newDecoder accepted a 65535x65535 header; decode would allocate 4GB")
	}
}

// palettedImage wraps an *image.Paletted so that Encode takes the generic
// image.PalettedImage branch instead of the *image.Paletted one.
type palettedImage struct {
	*image.Paletted
}

// TestEncodeLargePalette covers a palette longer than a PCX extended palette can
// hold. image.Paletted permits it, and the encoder used to index past the end of its
// output buffer and panic. Truncating to 256 entries is lossless: Pix is a []uint8,
// so no pixel can reference an entry above 255.
func TestEncodeLargePalette(t *testing.T) {
	t.Parallel()

	pal := make(color.Palette, 300)
	for i := range pal {
		pal[i] = color.RGBA{R: byte(i), G: byte(255 - i), B: byte(i * 7), A: 0xff}
	}
	in := image.NewPaletted(image.Rect(0, 0, 16, 4), pal)
	for i := range in.Pix {
		in.Pix[i] = byte(i * 3)
	}

	for _, tc := range []struct {
		name string
		img  image.Image
	}{
		{"image.Paletted", in},
		{"image.PalettedImage", palettedImage{in}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			buf := &bytes.Buffer{}
			if err := Encode(buf, tc.img); err != nil {
				t.Fatalf("Encode: %s", err)
			}
			decoded, err := Decode(buf)
			if err != nil {
				t.Fatalf("Decode: %s", err)
			}
			out, ok := decoded.(*image.Paletted)
			if !ok {
				t.Fatalf("decoded %T, expected *image.Paletted", decoded)
			}
			if len(out.Palette) != 256 {
				t.Errorf("output palette has %d entries, expected 256", len(out.Palette))
			}
			for i, want := range pal[:256] {
				if out.Palette[i] != want {
					t.Errorf("palette entry %d is %v, expected %v", i, out.Palette[i], want)
				}
			}
			if !bytes.Equal(in.Pix, out.Pix) {
				t.Error("pixel data differs")
			}
		})
	}
}

// TestEncodeInvalidBounds covers the bounds PCX cannot represent. Each has to be
// rejected before anything is written, with a plain error rather than one of the
// typed ones, which describe input *files* rather than caller mistakes.
func TestEncodeInvalidBounds(t *testing.T) {
	t.Parallel()

	for _, r := range []image.Rectangle{
		image.Rect(0, 0, 0, 0),   // empty
		image.Rect(0, 0, 0, 16),  // empty in one dimension only
		image.Rect(-4, -4, 4, 4), // PCX stores the origin unsigned
		image.Rect(0, 0, 70000, 1),
		image.Rect(0, 0, 1, 70000),
		image.Rect(0, 0, 65537, 1), // one past the largest representable width
	} {
		// Every encode path goes through writeHeader, so one bad rectangle has to be
		// rejected whichever concrete type carries it.
		for _, tc := range []struct {
			name string
			img  image.Image
		}{
			{"RGBA", image.NewRGBA(r)},
			{"Gray", image.NewGray(r)},
			{"Paletted", image.NewPaletted(r, color.Palette{color.Black, color.White})},
		} {
			t.Run(tc.name+r.String(), func(t *testing.T) {
				t.Parallel()
				buf := &bytes.Buffer{}
				err := Encode(buf, tc.img)
				if err == nil {
					t.Fatalf("Encode accepted bounds %v", r)
				}
				var (
					format      FormatError
					unsupported UnsupportedError
				)
				if errors.As(err, &format) || errors.As(err, &unsupported) {
					t.Errorf("err is %v (%T); caller mistakes use a plain error", err, err)
				}
				if buf.Len() != 0 {
					t.Errorf("wrote %d bytes before rejecting the image", buf.Len())
				}
			})
		}
	}
}

// TestEncodeMaximumBounds is the other side of the check above: the widest image each
// variant can represent still encodes. The limit is not the same for both, because
// bytes per line is also a 16-bit field: at 8bpp that caps the width at 65534, one
// byte per pixel rounded up to even, while the 1bpp variant packs eight pixels into a
// byte and so reaches the full 65536 the dimension fields allow.
func TestEncodeMaximumBounds(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		img  image.Image
	}{
		{"8bpp", image.NewGray(image.Rect(0, 0, 65534, 1))},
		{"1bpp", image.NewPaletted(image.Rect(0, 0, 65536, 1), color.Palette{color.Black, color.White})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			buf := &bytes.Buffer{}
			if err := Encode(buf, tc.img); err != nil {
				t.Fatalf("Encode of a %v image: %s", tc.img.Bounds(), err)
			}
		})
	}
}

// TestEncodeBytesPerLineLimit pins the boundary the test above describes: an 8bpp
// image one pixel wider needs 65536 bytes per line, which the header cannot hold.
func TestEncodeBytesPerLineLimit(t *testing.T) {
	t.Parallel()

	buf := &bytes.Buffer{}
	if err := Encode(buf, image.NewGray(image.Rect(0, 0, 65535, 1))); err == nil {
		t.Error("Encode accepted an 8bpp image needing 65536 bytes per line")
	} else if buf.Len() != 0 {
		t.Errorf("wrote %d bytes before rejecting the image", buf.Len())
	}
}
