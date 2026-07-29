// Package pcx implements a PCX image decoder and encoder.
//
// Specification: http://web.archive.org/web/20030111010058/http://www.nist.fss.ru/hr/doc/spec/pcx.htm
// Updated description: https://moddingwiki.shikadi.net/wiki/PCX_Format
//
// Sample files: http://samples.ffmpeg.org/image-samples/pcx/
package pcx

import (
	"bufio"
	"errors"
	"fmt"
	"image"
	"image/color"
	"io"
)

// Version:
// 0 = Version 2.5 of PC Paintbrush
// 2 = Version 2.8 w/palette information
// 3 = Version 2.8 w/o palette information
// 4 = PC Paintbrush for Windows(Plus for Windows uses Ver 5)
// 5 = Version 3.0 and > of PC Paintbrush
//    and PC Paintbrush +, includes
//    Publisher's Paintbrush . Includes
//    24-bit .PCX files

const (
	magic        = 0x0a
	paletteMagic = 0x0c
)

// Values for the palette info byte at header offset 68.
const (
	paletteInfoColor     = 1
	paletteInfoGrayscale = 2
)

// maxPixels bounds the pixel count the decoder will allocate for. PCX
// dimensions are 16-bit, so a 128-byte header can otherwise request
// 65536*65536 pixels — 4GB as Gray, 17GB as RGBA.
const maxPixels = 1 << 28

type decoder struct {
	r                io.Reader
	version          int
	rle              bool
	bpp              int
	bounds           image.Rectangle
	horizDPI         int
	vertDPI          int
	horizScreenSize  int
	vertScreenSize   int
	colormap         [48]byte
	nplanes          int
	bytesPerLine     int
	bytesPerScanline int
	paletteInfo      int
	pb4              bool
	colorModel       color.Model
	// palette is the palette carried in the header colormap, set by readHeader for
	// the variants that have one. It is nil for the 8bpp variants, whose palette is
	// either absent or stored after the pixel data.
	palette color.Palette
}

type Image struct {
	image.Image

	Version                         int
	Paintbrush4                     bool
	HorizDPI, VertDPI               int
	HorizScreenSize, VertScreenSize int
}

// A FormatError reports that the input is not a valid PCX.
type FormatError string

func (e FormatError) Error() string {
	return "pcx: invalid format: " + string(e)
}

// An UnsupportedError reports that the input uses a valid but unimplemented PCX feature.
type UnsupportedError string

func (e UnsupportedError) Error() string {
	return "pcx: unsupported variant: " + string(e)
}

func init() {
	// The magic also matches the RLE bit to make sure it's set
	image.RegisterFormat("pcx", "\x0a?\x01", Decode, DecodeConfig)
}

// Decode reads a PCX image from r and returns it as an image.Image.
// The type of Image returned depends on the PCX contents. The possible
// image types are returned are defined in the stdlib image package. Use
// DecodeExtended to preserve more information such as DPI.
func Decode(r io.Reader) (image.Image, error) {
	d, err := newDecoder(r)
	if err != nil {
		return nil, err
	}
	img, err := d.decode()
	if err != nil {
		return nil, err
	}
	return img, nil
}

// DecodeConfig returns the color model and dimensions of a PCX image
// without decoding the entire image.
//
// For the variants whose palette lives in the 128-byte header — 1bpp mono, CGA,
// EGA and the planar variants — the reported ColorModel is the color.Palette
// Decode will use. The 8 bits per pixel single plane variant stores its palette
// after the pixel data, so it cannot be reported here: that variant reports
// color.RGBAModel even though Decode returns an *image.Paletted (or an
// *image.Gray for a grayscale file carrying no palette).
func DecodeConfig(r io.Reader) (image.Config, error) {
	d, err := newDecoder(r)
	if err != nil {
		return image.Config{}, err
	}
	return image.Config{
		ColorModel: d.colorModel,
		Width:      d.bounds.Dx(),
		Height:     d.bounds.Dy(),
	}, nil
}

// DecodeExtended is similar to Decode except that the returned image is an
// internal representation that maintains more metadata such as DPI.
func DecodeExtended(r io.Reader) (*Image, error) {
	d, err := newDecoder(r)
	if err != nil {
		return nil, err
	}
	img, err := d.decode()
	if err != nil {
		return nil, err
	}
	return &Image{
		Image:           img,
		Version:         d.version,
		Paintbrush4:     d.pb4,
		HorizDPI:        d.horizDPI,
		VertDPI:         d.vertDPI,
		HorizScreenSize: d.horizScreenSize,
		VertScreenSize:  d.vertScreenSize,
	}, nil
}

// newDecoder initializes a new decoder and reads the PCX
// header from the provided reader. It will return an error
// if the header is invalid, but it does not validate the
// image further.
func newDecoder(r io.Reader) (*decoder, error) {
	d := &decoder{
		r: r,
	}
	if err := d.readHeader(); err != nil {
		return nil, unexpectedEOF(err)
	}
	return d, nil
}

func (d *decoder) readHeader() error {
	var buf [128]byte

	_, err := io.ReadFull(d.r, buf[:128])
	if err != nil {
		return err
	}

	if buf[0] != magic {
		return FormatError("not a PCX file")
	}

	d.version = int(buf[1])
	d.rle = buf[2] == 1
	if !d.rle {
		// Rejected here rather than in decode so DecodeConfig doesn't report the
		// dimensions of a file Decode then refuses.
		return UnsupportedError("non-RLE")
	}
	d.bpp = int(buf[3])
	switch d.bpp {
	case 1, 2, 4, 8:
	default:
		return FormatError(fmt.Sprintf("unsupported bpp (%d)", d.bpp))
	}
	d.bounds = image.Rect(
		int(buf[4])|(int(buf[5])<<8),
		int(buf[6])|(int(buf[7])<<8),
		(int(buf[8])|(int(buf[9])<<8))+1,
		(int(buf[10])|(int(buf[11])<<8))+1)
	d.horizDPI = int(buf[12]) | (int(buf[13]) << 8)
	d.vertDPI = int(buf[14]) | (int(buf[15]) << 8)
	copy(d.colormap[:48], buf[16:16+48])
	// Byte 64 is reserved and should be set to 0
	d.nplanes = int(buf[65])
	if d.nplanes == 0 {
		// The upper bound is left to decode() so that unimplemented plane
		// counts keep reporting UnsupportedError rather than FormatError.
		return FormatError("no planes")
	}
	// The number of bytes per line MUST be an even number.
	d.bytesPerLine = int(buf[66]) | (int(buf[67]) << 8)
	d.bytesPerScanline = d.bytesPerLine * d.nplanes
	// A paletteInfo of 2 marks the image as grayscale. It doesn't change the encoding,
	// so it only matters for the 8bpp single plane variant, where it distinguishes a
	// grayscale image from one whose palette is missing. PB IV/IV+ ignore it entirely.
	d.paletteInfo = int(buf[68])
	d.pb4 = buf[68] != 0
	// horizontal and vertical screen size in pixels are found only in PB IV/IV Plus
	d.horizScreenSize = int(buf[70]) | (int(buf[71]) << 8)
	d.vertScreenSize = int(buf[72]) | (int(buf[73]) << 8)
	// The rest of the 54 bytes should be all set to 0

	if d.bounds.Dx() <= 0 || d.bounds.Dy() <= 0 {
		return FormatError(fmt.Sprintf("invalid image dimensions %d x %d", d.bounds.Dx(), d.bounds.Dy()))
	}

	if d.bytesPerScanline < (d.bounds.Dx()*d.bpp*d.nplanes+7)/8 {
		return FormatError("corrupt image")
	}

	// Computed in 64-bit so the product can't wrap on 32-bit builds.
	if int64(d.bounds.Dx())*int64(d.bounds.Dy()) > maxPixels {
		return FormatError(fmt.Sprintf("image too large %d x %d", d.bounds.Dx(), d.bounds.Dy()))
	}

	switch {
	case d.hasHeaderPalette():
		// The palette is in the colormap, so it is known without reading the pixel
		// data and color.Palette is the exact color model.
		d.palette, err = d.headerPalette()
		if err != nil {
			return err
		}
		d.colorModel = d.palette
	case d.bpp == 8 && d.nplanes == 4:
		// The four plane truecolor variant carries an alpha plane and decodes to
		// *image.NRGBA.
		d.colorModel = color.NRGBAModel
	default:
		// Every other variant is opaque. See the note on the 8bpp single plane
		// limitation in DecodeConfig.
		d.colorModel = color.RGBAModel
	}

	return nil
}

// hasHeaderPalette reports whether this variant's palette comes from the 48 byte
// colormap in the header, and so is known before the pixel data is read. Only the
// variants decode routes to decodePaletted or decodePlanar qualify; the 8bpp
// single plane variant's palette follows the pixel data.
func (d *decoder) hasHeaderPalette() bool {
	return (d.nplanes == 1 && d.bpp < 8) || (d.bpp == 1 && d.nplanes >= 2 && d.nplanes <= 4)
}

// headerPalette builds the palette for the variants hasHeaderPalette reports on.
// The decoder always returns a full 1<<bpp (or 1<<nplanes for the planar variants)
// entries, since a pixel value can index any of them.
func (d *decoder) headerPalette() (color.Palette, error) {
	bits := d.bpp
	if d.nplanes > 1 {
		bits = d.nplanes
	}
	pal := make(color.Palette, 1<<uint(bits))
	switch {
	case d.nplanes == 1 && d.bpp == 1: // B&W
		pal[0] = color.Black
		pal[1] = color.White
	case d.nplanes == 1 && d.bpp == 2 && d.bounds.Dx() == 320 && d.bounds.Dy() == 200: // CGA
		// The background is the high nibble of the first colormap entry's red byte in
		// both encodings.
		pal[0] = cga16ColorPalette[d.colormap[0]>>4]
		// PC Paintbrush 3.0 stores nothing but three flag bits in colormap[3] and leaves
		// the rest of the second entry zero, so any other bit set there means the entry
		// holds a real RGB colour and the 4.0 encoding is in use. Byte 68 is no signal
		// here: PB 3.0 and later set it on any colour file, CGA or not.
		var idx int
		if d.colormap[3]&0x1f != 0 || d.colormap[4] != 0 || d.colormap[5] != 0 {
			// PC Paintbush 4.0 writes the approximate RGB of palette entry 1 and the
			// palette is recovered from it. Described in
			// https://moddingwiki.shikadi.net/wiki/PCX_Format#Newer_method
			// G = colormap[4]
			// B = colormap[5]
			// Pick green palette if G>B in slot 1
			// Pick bright palette if max(G,B) > 200
			// There is no colour burst bit, so only palettes 0-3 are reachable.
			if d.colormap[5] >= d.colormap[4] {
				idx |= 2
			}
			if max(d.colormap[5], d.colormap[4]) > 200 {
				idx |= 1
			}
		} else {
			// PC Paintbrush 3.0: colour burst, palette and intensity in bits 7-5.
			idx = int(d.colormap[3] >> 5)
		}
		copy(pal[1:], cga4ColorPalettes[idx])
	default: // EGA, and the planar variants
		if len(pal)*3 > len(d.colormap) {
			return nil, FormatError(fmt.Sprintf("palette larger (%d) than available colormap (%d)", len(pal)*3, len(d.colormap)))
		}
		for i := 0; i < len(pal)*3; i += 3 {
			pal[i/3] = color.RGBA{R: d.colormap[i], G: d.colormap[i+1], B: d.colormap[i+2], A: 255}
		}
	}
	return pal, nil
}

func (d *decoder) decode() (image.Image, error) {
	// Non-RLE input is rejected by readHeader, so d.rle is always set here.
	bufR := bufio.NewReader(d.r)

	switch {
	case d.nplanes == 1:
		if d.bpp == 8 {
			return d.decode8bpp1plane(bufR)
		}
		return d.decodePaletted(bufR)
	case d.bpp == 8 && (d.nplanes == 3 || d.nplanes == 4):
		return d.decodeRGB(bufR)
	case d.bpp == 1 && (d.nplanes >= 2 && d.nplanes <= 4):
		return d.decodePlanar(bufR)
	}

	return nil, UnsupportedError(fmt.Sprintf("version %d with %d planes %d bpp", d.version, d.nplanes, d.bpp))
}

// decode8bpp1plane decodes the 8 bits per pixel, single plane variant. The pixel
// data is byte for byte identical whether the file is paletted or grayscale, so the
// concrete image type is decided afterwards by whether the file actually carries a
// trailing extended palette rather than by the header's version or palette info.
func (d *decoder) decode8bpp1plane(bufR *bufio.Reader) (image.Image, error) {
	w, h := d.bounds.Dx(), d.bounds.Dy()
	pix := make([]byte, w*h)
	pad := d.bytesPerScanline - w
	for y := range h {
		if err := rleDecode(bufR, pix[y*w:(y+1)*w], pad); err != nil {
			return nil, err
		}
	}

	pal, err := d.readExtendedPalette(bufR)
	if err != nil {
		return nil, err
	}
	// image.Gray and image.Paletted share the same 1-byte-per-pixel layout,
	// so pix can back either without a second allocation.
	if pal == nil {
		if d.paletteInfo != paletteInfoGrayscale {
			return nil, FormatError("missing extended palette")
		}
		// Grayscale: the pixel value is the intensity, so no palette is needed.
		return &image.Gray{Pix: pix, Stride: w, Rect: d.bounds}, nil
	}
	return &image.Paletted{Pix: pix, Stride: w, Rect: d.bounds, Palette: pal}, nil
}

// readExtendedPalette reads the 768 byte palette that 8 bits per pixel files carry
// after the pixel data, introduced by a paletteMagic byte. It returns a nil palette
// and no error when the file carries no palette at all, leaving it to the caller to
// decide whether that means grayscale or a malformed file.
func (d *decoder) readExtendedPalette(bufR *bufio.Reader) (color.Palette, error) {
	switch by, err := bufR.ReadByte(); {
	case errors.Is(err, io.EOF):
		return nil, nil
	case err != nil:
		return nil, err
	case by != paletteMagic:
		return nil, nil
	}

	var palBytes [3 * 256]byte
	if _, err := io.ReadFull(bufR, palBytes[:]); err != nil {
		return nil, unexpectedEOF(err)
	}
	pal := make(color.Palette, 256)
	for i := range pal {
		pal[i] = color.RGBA{R: palBytes[i*3], G: palBytes[i*3+1], B: palBytes[i*3+2], A: 255}
	}
	return pal, nil
}

// decodeRGB decodes the 8 bits per pixel, three or four plane truecolor variants.
// image.RGBA and image.NRGBA share the same 4-byte-per-pixel layout, so the same
// pixel buffer backs either.
func (d *decoder) decodeRGB(bufR *bufio.Reader) (image.Image, error) {
	width := d.bounds.Dx()
	height := d.bounds.Dy()
	stride := 4 * width
	pix := make([]byte, stride*height)
	offset := 0
	buf := make([]byte, d.bytesPerScanline)
	for range height {
		if err := rleDecode(bufR, buf, 0); err != nil {
			return nil, err
		}
		for x := range width {
			pix[offset] = buf[x]
			pix[offset+1] = buf[x+d.bytesPerLine]
			pix[offset+2] = buf[x+2*d.bytesPerLine]
			if d.nplanes == 4 {
				pix[offset+3] = buf[x+3*d.bytesPerLine]
			} else {
				pix[offset+3] = 255
			}
			offset += 4
		}
	}
	if d.nplanes == 4 {
		// A fourth plane holds straight, not premultiplied, alpha, which is what
		// image.NRGBA stores. Putting it in an image.RGBA would break that type's
		// premultiplied invariant: a value above the alpha is not representable.
		return &image.NRGBA{Pix: pix, Stride: stride, Rect: d.bounds}, nil
	}
	// Three planes are always fully opaque, where premultiplied and straight alpha
	// coincide, so the more widely handled image.RGBA is used.
	return &image.RGBA{Pix: pix, Stride: stride, Rect: d.bounds}, nil
}

// decodePaletted decodes the single plane variants below 8 bits per pixel, whose
// palette readHeader has already built from the header colormap.
func (d *decoder) decodePaletted(bufR *bufio.Reader) (image.Image, error) {
	img := image.NewPaletted(d.bounds, d.palette)
	width, height := d.bounds.Dx(), d.bounds.Dy()
	buf := make([]byte, d.bytesPerScanline)
	mask := byte((1 << uint(d.bpp)) - 1)
	for y := range height {
		if err := rleDecode(bufR, buf, 0); err != nil {
			return nil, err
		}
		shift := byte(8 - d.bpp)
		for x, o := 0, 0; x < width; x++ {
			img.Pix[y*img.Stride+x] = (buf[o] >> shift) & mask
			if shift == 0 {
				o++
				shift = byte(8 - d.bpp)
			} else {
				shift -= byte(d.bpp)
			}
		}
	}

	return img, nil
}

// decodePlanar decodes the 1 bit per pixel multi-plane variants, whose palette
// readHeader has already built from the header colormap.
func (d *decoder) decodePlanar(bufR *bufio.Reader) (image.Image, error) {
	img := image.NewPaletted(d.bounds, d.palette)

	width := d.bounds.Dx()
	height := d.bounds.Dy()
	buf := make([]byte, d.bytesPerScanline)
	for y := range height {
		if err := rleDecode(bufR, buf, 0); err != nil {
			return nil, err
		}
		for x := range width {
			var v byte
			for i := range d.nplanes {
				v = (v >> 1) | ((buf[d.bytesPerLine*i+(x/8)] << (uint(x) & 7)) & 0x80)
			}
			v >>= uint(8 - d.nplanes)
			img.Pix[y*img.Stride+x] = v
		}
	}
	return img, nil
}
