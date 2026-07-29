package pcx

import (
	"bytes"
	"image"
	"os"
	"path/filepath"
	"testing"
)

func FuzzDecodeEncode(f *testing.F) {
	testImages, err := filepath.Glob("testdata/*.pcx")
	if err != nil {
		f.Fatalf("Failed to list test files: %s", err)
	}
	for _, path := range testImages {
		b, err := os.ReadFile(path)
		if err != nil {
			f.Fatalf("Failed to decode %s: %s", path, err)
		}
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		d, err := newDecoder(bytes.NewReader(data))
		if err != nil {
			return
		}
		// The decoder's own maxPixels ceiling is what keeps a hostile header from
		// asking for unbounded memory, and it is pinned deterministically by
		// TestDecodeMalformedHeader and TestDecodeHugeHeaderAllocatesNothing rather
		// than left to this fuzzer to rediscover. What that ceiling still permits is
		// a gigabyte of pixel buffer for a 128-byte input, which every worker then
		// spends its time allocating and zeroing. None of the decode paths depend on
		// the size of the image, so the largest ones are skipped here: this bound is
		// three times the largest fixture (307,200 pixels).
		if int64(d.bounds.Dx())*int64(d.bounds.Dy()) > 1<<20 {
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
		if img.Bounds() != img1.Bounds() {
			t.Fatalf("Bounds of decoded image %v don't match encoded image %v",
				img.Bounds(), img1.Bounds())
		}
		comparePixels(t, d, img, img1)
	})
}

// comparePixels checks that a decode/encode/decode round trip preserved the image.
//
// The concrete type is part of the comparison, since only one round trip changes it,
// deliberately: an opaque *image.NRGBA is re-encoded as three planes and comes back
// *image.RGBA.
func comparePixels(t *testing.T, d *decoder, in, out image.Image) {
	t.Helper()
	switch in := in.(type) {
	case *image.Paletted:
		out, ok := out.(*image.Paletted)
		if !ok {
			t.Fatalf("Expected *image.Paletted, got %T", out)
		}
		if !bytes.Equal(in.Pix, out.Pix) {
			t.Fatal("Pixel indices of *image.Paletted differ")
		}
		if d.bpp == 2 && d.nplanes == 1 && in.Bounds().Dx() == 320 && in.Bounds().Dy() == 200 {
			// Any 2bpp 320x200 file is read as CGA with its palette rebuilt from the
			// CGA tables, and the color burst palettes have no form the encoder can
			// write back. Palette fidelity for this shape is pinned by the cga_*
			// fixtures instead.
			return
		}
		// A palette only ever rounds up to the next size its variant can hold.
		if len(out.Palette) < len(in.Palette) {
			t.Fatalf("Expected at least %d palette entries, got %d", len(in.Palette), len(out.Palette))
		}
		for i, want := range in.Palette {
			if c := out.Palette[i]; c != want {
				t.Fatalf("Palette entry %d is %v, expected %v", i, c, want)
			}
		}
	case *image.Gray:
		out, ok := out.(*image.Gray)
		if !ok {
			t.Fatalf("Expected *image.Gray, got %T", out)
		}
		if !bytes.Equal(in.Pix, out.Pix) {
			t.Fatal("Pixels of *image.Gray differ")
		}
	case *image.RGBA:
		// A three plane file is always opaque, so it re-encodes as three planes.
		out, ok := out.(*image.RGBA)
		if !ok {
			t.Fatalf("Expected *image.RGBA, got %T", out)
		}
		if !bytes.Equal(in.Pix, out.Pix) {
			t.Fatal("Pixels of *image.RGBA differ")
		}
	case *image.NRGBA:
		// A four plane file that happens to be opaque loses its alpha plane on the
		// way out, so the type changes; the values must not.
		var pix []byte
		if in.Opaque() {
			o, ok := out.(*image.RGBA)
			if !ok {
				t.Fatalf("Expected an opaque *image.NRGBA to re-encode as *image.RGBA, got %T", out)
			}
			pix = o.Pix
		} else {
			o, ok := out.(*image.NRGBA)
			if !ok {
				t.Fatalf("Expected *image.NRGBA, got %T", out)
			}
			pix = o.Pix
		}
		if !bytes.Equal(in.Pix, pix) {
			t.Fatal("Pixels of *image.NRGBA differ")
		}
	default:
		t.Fatalf("Unhandled image type %T", in)
	}
}
