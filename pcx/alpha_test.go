package pcx

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"
)

// TestDecodeRGBA covers the 8bpp four plane variant, whose fourth plane holds
// straight alpha. See testdata/rgba-pcx.txt for how the fixture was produced.
func TestDecodeRGBA(t *testing.T) {
	t.Parallel()

	f, err := os.Open("testdata/rgba8.png")
	if err != nil {
		t.Fatal(err)
	}
	refImg, err := png.Decode(f)
	if err := f.Close(); err != nil {
		t.Error(err)
	}
	if err != nil {
		t.Fatal(err)
	}
	ref, ok := refImg.(*image.NRGBA)
	if !ok {
		t.Fatalf("reference image is %T, expected *image.NRGBA", refImg)
	}

	b, err := os.ReadFile("testdata/rgba8.pcx")
	if err != nil {
		t.Fatal(err)
	}
	img, err := Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("failed to decode: %s", err)
	}
	// Straight alpha in an *image.RGBA would violate that type's premultiplied
	// invariant, so a four plane file has to decode as *image.NRGBA.
	got, ok := img.(*image.NRGBA)
	if !ok {
		t.Fatalf("expected *image.NRGBA, got %T", img)
	}
	if got.Bounds() != ref.Bounds() {
		t.Fatalf("bounds are %v, expected %v", got.Bounds(), ref.Bounds())
	}
	if !bytes.Equal(got.Pix, ref.Pix) {
		t.Fatal("pixels differ from testdata/rgba8.png")
	}
	if ref.Opaque() {
		t.Fatal("the fixture is fully opaque, so it no longer tests anything")
	}

	// DecodeConfig must agree about the color model.
	cfg, err := DecodeConfig(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("failed to decode config: %s", err)
	}
	if cfg.ColorModel != color.NRGBAModel {
		t.Errorf("config color model is %v, expected color.NRGBAModel", cfg.ColorModel)
	}
}

// alphaTestImage builds a w x h image with a translucent first pixel, in each of the
// truecolor types the encoder has a path for. Odd widths exercise the scanline
// padding, since BytesPerLine has to be even.
func alphaTestImage(kind string, w, h int) image.Image {
	r := image.Rect(0, 0, w, h)
	switch kind {
	case "NRGBA":
		m := image.NewNRGBA(r)
		for y := range h {
			for x := range w {
				m.SetNRGBA(x, y, color.NRGBA{R: byte(200 - x), G: byte(100 + y), B: 50, A: 255})
			}
		}
		m.SetNRGBA(0, 0, color.NRGBA{R: 200, G: 100, B: 50, A: 128})
		return m
	case "NRGBA64":
		m := image.NewNRGBA64(r)
		for y := range h {
			for x := range w {
				m.SetNRGBA64(x, y, color.NRGBA64{
					R: uint16(200-x) * 0x101, G: uint16(100+y) * 0x101, B: 50 * 0x101, A: 0xffff})
			}
		}
		m.SetNRGBA64(0, 0, color.NRGBA64{R: 200 * 0x101, G: 100 * 0x101, B: 50 * 0x101, A: 128 * 0x101})
		return m
	case "RGBA":
		// Premultiplied, so the translucent pixel's stored color is halved.
		m := image.NewRGBA(r)
		for y := range h {
			for x := range w {
				m.SetRGBA(x, y, color.RGBA{R: byte(200 - x), G: byte(100 + y), B: 50, A: 255})
			}
		}
		m.SetRGBA(0, 0, color.RGBA{R: 100, G: 50, B: 25, A: 128})
		return m
	}
	panic("unknown kind " + kind)
}

// TestEncodeAlpha checks that alpha survives a round trip through the encoder. All
// three source types decode back as *image.NRGBA, since that is what the four plane
// variant carries, and the color values must come back un-darkened: writing
// premultiplied values into the file is what used to turn {200,100,50,128} into
// {100,50,25,255}.
func TestEncodeAlpha(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{"NRGBA", "NRGBA64", "RGBA"} {
		for _, w := range []int{1, 3, 4, 17} {
			t.Run(fmt.Sprintf("%s/%dx5", kind, w), func(t *testing.T) {
				in := alphaTestImage(kind, w, 5)
				buf := &bytes.Buffer{}
				if err := Encode(buf, in); err != nil {
					t.Fatalf("failed to encode: %s", err)
				}
				// A translucent image needs the four plane form.
				if np := buf.Bytes()[65]; np != 4 {
					t.Fatalf("encoded with %d planes, expected 4", np)
				}
				img, err := Decode(buf)
				if err != nil {
					t.Fatalf("failed to decode after encode: %s", err)
				}
				out, ok := img.(*image.NRGBA)
				if !ok {
					t.Fatalf("expected *image.NRGBA, got %T", img)
				}
				if out.Bounds() != in.Bounds() {
					t.Fatalf("bounds are %v, expected %v", out.Bounds(), in.Bounds())
				}
				// The straight alpha types must come back exactly. An *image.RGBA
				// source cannot: premultiplying {200,100,50} by 128/255 already lost
				// the low bits before the encoder ever saw it, so the check there is
				// only that the color was not halved a second time.
				switch c := out.NRGBAAt(0, 0); {
				case kind != "RGBA":
					if c != (color.NRGBA{R: 200, G: 100, B: 50, A: 128}) {
						t.Errorf("translucent pixel is %v, expected {200 100 50 128}", c)
					}
				case c.R < 199 || c.R > 200 || c.A != 128:
					t.Errorf("translucent pixel is %v, expected roughly {200 100 50 128}", c)
				}
				b := in.Bounds()
				for y := b.Min.Y; y < b.Max.Y; y++ {
					for x := b.Min.X; x < b.Max.X; x++ {
						want := color.NRGBAModel.Convert(in.At(x, y))
						if got := out.NRGBAAt(x, y); got != want {
							t.Fatalf("pixel (%d,%d) is %v, expected %v", x, y, got, want)
						}
					}
				}
			})
		}
	}
}

// plainImage hides the Opaque method of the image it wraps, leaving the encoder with
// nothing but At to decide the plane count.
type plainImage struct{ m image.Image }

func (p plainImage) ColorModel() color.Model { return p.m.ColorModel() }
func (p plainImage) Bounds() image.Rectangle { return p.m.Bounds() }
func (p plainImage) At(x, y int) color.Color { return p.m.At(x, y) }

// TestEncodeAlphaForeignImage covers the per-pixel opacity scan the encoder falls back
// to for image types that do not implement Opaque.
func TestEncodeAlphaForeignImage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		nplanes byte
		img     image.Image
	}{
		{name: "translucent", nplanes: 4, img: alphaTestImage("NRGBA", 5, 3)},
		{name: "opaque", nplanes: 3, img: image.NewNRGBA(image.Rect(0, 0, 5, 3))},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.name == "opaque" {
				// A zero NRGBA is transparent, so fill it in.
				m := test.img.(*image.NRGBA)
				for i := 3; i < len(m.Pix); i += 4 {
					m.Pix[i] = 255
				}
			}
			buf := &bytes.Buffer{}
			if err := Encode(buf, plainImage{test.img}); err != nil {
				t.Fatalf("failed to encode: %s", err)
			}
			if np := buf.Bytes()[65]; np != test.nplanes {
				t.Fatalf("encoded with %d planes, expected %d", np, test.nplanes)
			}
			img, err := Decode(buf)
			if err != nil {
				t.Fatalf("failed to decode after encode: %s", err)
			}
			b := test.img.Bounds()
			for y := b.Min.Y; y < b.Max.Y; y++ {
				for x := b.Min.X; x < b.Max.X; x++ {
					want := color.NRGBAModel.Convert(test.img.At(x, y))
					if got := color.NRGBAModel.Convert(img.At(x, y)); got != want {
						t.Fatalf("pixel (%d,%d) is %v, expected %v", x, y, got, want)
					}
				}
			}
		})
	}
}

// TestEncodeOpaqueAlphaTypes checks that an opaque image is still written in the
// three plane form no PCX reader can be confused by, whatever type it came in as.
// *image.NRGBA and the generic At based path therefore decode back as *image.RGBA:
// the type changes but every value is preserved, since alpha is 255 throughout.
func TestEncodeOpaqueAlphaTypes(t *testing.T) {
	t.Parallel()

	opaque := func(m image.Image) image.Image {
		b := m.Bounds()
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				switch m := m.(type) {
				case *image.NRGBA:
					m.SetNRGBA(x, y, color.NRGBA{R: byte(x), G: byte(y), B: 50, A: 255})
				case *image.NRGBA64:
					m.SetNRGBA64(x, y, color.NRGBA64{R: uint16(x) * 0x101, G: uint16(y) * 0x101, B: 50 * 0x101, A: 0xffff})
				case *image.RGBA:
					m.SetRGBA(x, y, color.RGBA{R: byte(x), G: byte(y), B: 50, A: 255})
				}
			}
		}
		return m
	}

	for _, kind := range []string{"NRGBA", "NRGBA64", "RGBA"} {
		t.Run(kind, func(t *testing.T) {
			in := opaque(alphaTestImage(kind, 7, 5))
			buf := &bytes.Buffer{}
			if err := Encode(buf, in); err != nil {
				t.Fatalf("failed to encode: %s", err)
			}
			if np := buf.Bytes()[65]; np != 3 {
				t.Fatalf("encoded with %d planes, expected 3", np)
			}
			img, err := Decode(buf)
			if err != nil {
				t.Fatalf("failed to decode after encode: %s", err)
			}
			out, ok := img.(*image.RGBA)
			if !ok {
				t.Fatalf("expected *image.RGBA, got %T", img)
			}
			b := in.Bounds()
			for y := b.Min.Y; y < b.Max.Y; y++ {
				for x := b.Min.X; x < b.Max.X; x++ {
					want := color.RGBAModel.Convert(in.At(x, y))
					if got := out.RGBAAt(x, y); got != want {
						t.Fatalf("pixel (%d,%d) is %v, expected %v", x, y, got, want)
					}
				}
			}
		})
	}
}
