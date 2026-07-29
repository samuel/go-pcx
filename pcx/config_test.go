package pcx

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDecodeConfigColorModel pins the contract DecodeConfig documents: for every
// variant whose palette is in the header, the reported color model is the exact
// palette Decode goes on to use. The 8bpp single plane variant can't be checked
// that way — its palette follows the pixel data — so it reports color.RGBAModel
// even when Decode returns an *image.Paletted.
func TestDecodeConfigColorModel(t *testing.T) {
	t.Parallel()

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
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := DecodeConfig(bytes.NewReader(b))
			if err != nil {
				t.Fatal(err)
			}
			img, err := Decode(bytes.NewReader(b))
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Width != img.Bounds().Dx() || cfg.Height != img.Bounds().Dy() {
				t.Errorf("config is %dx%d, image is %v", cfg.Width, cfg.Height, img.Bounds())
			}

			d, err := newDecoder(bytes.NewReader(b))
			if err != nil {
				t.Fatal(err)
			}
			switch {
			case d.hasHeaderPalette():
				pal, ok := cfg.ColorModel.(color.Palette)
				if !ok {
					t.Fatalf("color model is %T, expected color.Palette", cfg.ColorModel)
				}
				pimg, ok := img.(*image.Paletted)
				if !ok {
					t.Fatalf("decoded %T, expected *image.Paletted", img)
				}
				if len(pal) != len(pimg.Palette) {
					t.Fatalf("config palette has %d entries, image has %d", len(pal), len(pimg.Palette))
				}
				for i, c := range pal {
					if c != pimg.Palette[i] {
						t.Errorf("palette entry %d is %v, image has %v", i, c, pimg.Palette[i])
					}
				}
			case d.bpp == 8 && d.nplanes == 4:
				if cfg.ColorModel != color.NRGBAModel {
					t.Errorf("color model is %v, expected color.NRGBAModel", cfg.ColorModel)
				}
			default:
				// The documented 8bpp single plane limitation.
				if cfg.ColorModel != color.RGBAModel {
					t.Errorf("color model is %v, expected color.RGBAModel", cfg.ColorModel)
				}
			}
		})
	}
}

// TestDecodeTruncated checks that a file cut short mid-body reports
// io.ErrUnexpectedEOF rather than a bare io.EOF, which a caller reading a stream
// would take for a clean end of input.
func TestDecodeTruncated(t *testing.T) {
	t.Parallel()

	b, err := os.ReadFile("testdata/vball.pcx")
	if err != nil {
		t.Fatal(err)
	}
	// Just past the header, so the first scanline runs out of input.
	for _, n := range []int{129, 200, len(b) - 1} {
		_, err := Decode(bytes.NewReader(b[:n]))
		if !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Errorf("truncated to %d bytes: err is %v (%T), expected io.ErrUnexpectedEOF", n, err, err)
		}
		if errors.Is(err, io.EOF) {
			t.Errorf("truncated to %d bytes: err is io.EOF, which reads as a clean end of stream", n)
		}
	}
	// A header cut short is the same class of failure.
	if _, err := Decode(bytes.NewReader(b[:64])); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("truncated header: err is %v, expected io.ErrUnexpectedEOF", err)
	}
}

// TestDecodeNonRLE checks that the unsupported non-RLE encoding is rejected by the
// header read, so DecodeConfig doesn't describe a file Decode then refuses.
func TestDecodeNonRLE(t *testing.T) {
	t.Parallel()

	b, err := os.ReadFile("testdata/vball.pcx")
	if err != nil {
		t.Fatal(err)
	}
	b = bytes.Clone(b)
	b[2] = 0

	var unsupported UnsupportedError
	if _, err := Decode(bytes.NewReader(b)); !errors.As(err, &unsupported) {
		t.Errorf("Decode: err is %v (%T), expected UnsupportedError", err, err)
	}
	if _, err := DecodeConfig(bytes.NewReader(b)); !errors.As(err, &unsupported) {
		t.Errorf("DecodeConfig: err is %v (%T), expected UnsupportedError", err, err)
	}
}
