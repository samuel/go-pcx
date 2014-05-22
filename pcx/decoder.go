// Package pcx implements a PCX image decoder and encoder.
//
// Specification: http://web.archive.org/web/20030111010058/http://www.nist.fss.ru/hr/doc/spec/pcx.htm
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
// 4 = PC Paintbrush for Windows(Plus for
//    Windows uses Ver 5)
// 5 = Version 3.0 and > of PC Paintbrush
//    and PC Paintbrush +, includes
//    Publisher's Paintbrush . Includes
//    24-bit .PCX files

const (
	magic        = 0x0a
	paletteMagic = 0x0c
)

type decoder struct {
	r                io.Reader
	version          int
	rle              bool
	bpp              int
	bounds           image.Rectangle
	horizDpi         int
	vertDpi          int
	horizSize        int
	vertSize         int
	colormap         [48]byte
	nplanes          int
	bytesPerLine     int
	bytesPerScanline int
	grayscale        bool
	pb4              bool
	colorModel       color.Model
}

// A FormatError reports that the input is not a valid PCX.
type FormatError string

func (e FormatError) Error() string {
	return "pcx: invalid format: " + string(e)
}

// An UnsupportedError reports that the variant of the PCX file is not supported.
type UnsupportedError string

func (e UnsupportedError) Error() string {
	return "pcx: unsupported variant: " + string(e)
}

var cga16ColorPalette = [16]color.Color{
	color.RGBA{0x00, 0x00, 0x00, 0xff}, //  0 black
	color.RGBA{0x00, 0x00, 0xaa, 0xff}, //  1 blue
	color.RGBA{0x00, 0xaa, 0x00, 0xff}, //  2 green
	color.RGBA{0x00, 0xaa, 0xaa, 0xff}, //  3 cyan
	color.RGBA{0xaa, 0x00, 0x00, 0xff}, //  4 red
	color.RGBA{0xaa, 0x00, 0xaa, 0xff}, //  5 magenta
	color.RGBA{0xaa, 0x55, 0x00, 0xff}, //  6 brown
	color.RGBA{0xaa, 0xaa, 0xaa, 0xff}, //  7 light gray
	color.RGBA{0x55, 0x55, 0x55, 0xff}, //  8 gray
	color.RGBA{0x55, 0x55, 0xff, 0xff}, //  9 light blue
	color.RGBA{0x55, 0xff, 0x55, 0xff}, // 10 light green
	color.RGBA{0x55, 0xff, 0xff, 0xff}, // 11 light cyan
	color.RGBA{0xff, 0x55, 0x55, 0xff}, // 12 light red
	color.RGBA{0xff, 0x55, 0xff, 0xff}, // 13 light magenta
	color.RGBA{0xff, 0xff, 0x55, 0xff}, // 14 yellow
	color.RGBA{0xff, 0xff, 0xff, 0xff}, // 15 white
}

var cga4ColorPalettes = [8][]color.Color{
	[]color.Color{cga16ColorPalette[2], cga16ColorPalette[4], cga16ColorPalette[6]},    // green, red, brown
	[]color.Color{cga16ColorPalette[10], cga16ColorPalette[12], cga16ColorPalette[14]}, // light green, light red, yellow
	[]color.Color{cga16ColorPalette[3], cga16ColorPalette[5], cga16ColorPalette[7]},    // cyan, magenta, light gray
	[]color.Color{cga16ColorPalette[11], cga16ColorPalette[13], cga16ColorPalette[15]}, // light cyan, light magenta, white
	[]color.Color{cga16ColorPalette[3], cga16ColorPalette[4], cga16ColorPalette[7]},    // cyan, red, light gray
	[]color.Color{cga16ColorPalette[11], cga16ColorPalette[12], cga16ColorPalette[15]}, // light cyan, light red, white
	[]color.Color{cga16ColorPalette[3], cga16ColorPalette[4], cga16ColorPalette[7]},    // cyan, red, light gray
	[]color.Color{cga16ColorPalette[11], cga16ColorPalette[12], cga16ColorPalette[15]}, // light cyan, light red, white
}

func init() {
	// The magic also matches the RLE bit to make sure it's set
	image.RegisterFormat("pcx", "\x0a?\x01", Decode, DecodeConfig)
}

// Decode reads a PCX image from r and returns it as an image.Image.
// The type of Image returned depends on the PCX contents.
func Decode(r io.Reader) (image.Image, error) {
	d, err := newDecoder(r)
	if err != nil {
		return nil, err
	}
	return d.decode()
}

// DecodeConfig returns the color model and dimensions of a PCX image
// without decoding the entire image.
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

func newDecoder(r io.Reader) (*decoder, error) {
	d := &decoder{
		r: r,
	}
	if err := d.readHeader(); err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		return nil, err
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
	d.bpp = int(buf[3])
	if d.bpp < 1 || d.bpp > 8 {
		return FormatError(fmt.Sprintf("unsupported bpp (%d)", d.bpp))
	}
	var dim [4]int
	for i := 0; i < 4; i++ {
		dim[i] = int(buf[4+i*2]) | (int(buf[5+i*2]) << 8)
	}
	d.bounds = image.Rect(
		int(buf[4])|(int(buf[5])<<8),
		int(buf[6])|(int(buf[7])<<8),
		int(buf[8])|(int(buf[9])<<8)+1,
		int(buf[10])|(int(buf[11])<<8)+1)
	d.horizDpi = int(buf[12]) | (int(buf[13]) << 8)
	d.vertDpi = int(buf[14]) | (int(buf[15]) << 8)
	copy(d.colormap[:48], buf[16:16+48])
	d.nplanes = int(buf[65])
	d.bytesPerLine = int(buf[66]) | (int(buf[67]) << 8)
	d.bytesPerScanline = d.bytesPerLine * d.nplanes
	d.grayscale = buf[68] == 2
	d.pb4 = buf[68] != 0
	d.horizSize = int(buf[70]) | (int(buf[71]) << 8)
	d.vertSize = int(buf[72]) | (int(buf[73]) << 8)

	if d.bytesPerScanline < (d.bounds.Dx()*d.bpp*d.nplanes+7)/8 {
		return FormatError("corrupt image")
	}

	if d.grayscale {
		d.colorModel = color.GrayModel
	} else {
		d.colorModel = color.RGBAModel
	}

	return nil
}

func (d *decoder) decode() (image.Image, error) {
	if !d.rle {
		return nil, UnsupportedError("non-RLE")
	}

	switch {
	case d.colorModel == color.GrayModel:
		if d.bpp == 8 {
			return d.decodeGrayscale()
		}
		return nil, UnsupportedError("grayscale only supported with 8bpp")
	case d.nplanes == 1:
		if d.bpp == 8 {
			return d.decodeRGBPaletted()
		}
		return d.decodePaletted()
	case d.bpp == 8 && (d.nplanes == 3 || d.nplanes == 4):
		return d.decodeRGB()
	case d.bpp == 1 && (d.nplanes >= 2 && d.nplanes <= 4):
		return d.decodePlanar()
	}

	return nil, UnsupportedError(fmt.Sprintf("version %d with %d planes %d bpp", d.version, d.nplanes, d.bpp))
}

func (d *decoder) decodeGrayscale() (image.Image, error) {
	bufR := bufio.NewReader(d.r)
	img := image.NewGray(d.bounds)
	height := d.bounds.Dy()
	for y := 0; y < height; y++ {
		if err := d.rleDecode(bufR, img.Pix[y*img.Stride:]); err != nil {
			return img, err
		}
	}
	return img, nil
}

func (d *decoder) decodeRGB() (image.Image, error) {
	bufR := bufio.NewReader(d.r)

	img := image.NewRGBA(d.bounds)
	width := d.bounds.Dx()
	height := d.bounds.Dy()
	offset := 0
	buf := make([]byte, d.bytesPerScanline)
	for y := 0; y < height; y++ {
		if err := d.rleDecode(bufR, buf); err != nil {
			return img, err
		}
		for x := 0; x < width; x++ {
			img.Pix[offset] = buf[x]
			img.Pix[offset+1] = buf[x+d.bytesPerLine]
			img.Pix[offset+2] = buf[x+2*d.bytesPerLine]
			if d.nplanes == 4 {
				img.Pix[offset+3] = buf[x+3*d.bytesPerLine]
			} else {
				img.Pix[offset+3] = 255
			}
			offset += 4
		}
	}
	return img, nil
}

func (d *decoder) decodeRGBPaletted() (image.Image, error) {
	bufR := bufio.NewReader(d.r)

	pal := make([]color.Color, 256)
	img := image.NewPaletted(d.bounds, pal)
	height := d.bounds.Dy()
	for y := 0; y < height; y++ {
		if err := d.rleDecode(bufR, img.Pix[y*img.Stride:]); err != nil {
			return img, err
		}
	}

	// Read palette
	palBytes := make([]byte, 3*256)
	switch by, err := bufR.ReadByte(); {
	case (err == nil && by != paletteMagic) || err == io.EOF:
		return img, errors.New("pcx: missing extended palette")
	case err != nil:
		return img, err
	}
	if _, err := io.ReadFull(bufR, palBytes); err != nil {
		return img, err
	}
	for i := 0; i < 256; i++ {
		pal[i] = color.RGBA{R: palBytes[i*3], G: palBytes[i*3+1], B: palBytes[i*3+2], A: 255}
	}

	return img, nil
}

func (d *decoder) decodePaletted() (image.Image, error) {
	bufR := bufio.NewReader(d.r)

	pal := make([]color.Color, 1<<uint(d.bpp))
	switch {
	case d.bpp == 1: // B&W
		pal[0] = color.Black
		pal[1] = color.White
	case d.bpp == 2 && d.bounds.Dx() == 320 && d.bounds.Dy() == 200: // CGA
		pal[0] = cga16ColorPalette[d.colormap[0]>>4]
		idx := int(d.colormap[3] >> 5)
		if d.pb4 {
			// PC Paintbush 4.0 encodes the CGA palettes differently than 3.0.
			// Very thankful for the person that figured it out here:
			// https://github.com/wjaguar/mtPaint/blob/master/src/png.c
			i := 0
			if d.colormap[5] >= d.colormap[4] {
				i = 1
			}
			idx = i * 2
			if d.colormap[4+i] > 200 {
				idx++
			}
		}
		copy(pal[1:], cga4ColorPalettes[idx])
	default: // EGA
		for i := 0; i < len(pal)*3; i += 3 {
			pal[i/3] = color.RGBA{R: d.colormap[i], G: d.colormap[i+1], B: d.colormap[i+2], A: 255}
		}
	}

	img := image.NewPaletted(d.bounds, pal)
	width, height := d.bounds.Dx(), d.bounds.Dy()
	buf := make([]byte, d.bytesPerScanline)
	mask := byte((1 << uint(d.bpp)) - 1)
	for y := 0; y < height; y++ {
		if err := d.rleDecode(bufR, buf); err != nil {
			return img, err
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

func (d *decoder) decodePlanar() (image.Image, error) {
	pal := make([]color.Color, 1<<uint(d.nplanes))
	for i := 0; i < len(pal)*3; i += 3 {
		pal[i/3] = color.RGBA{R: d.colormap[i], G: d.colormap[i+1], B: d.colormap[i+2], A: 255}
	}
	img := image.NewPaletted(d.bounds, pal)

	bufR := bufio.NewReader(d.r)
	width := d.bounds.Dx()
	height := d.bounds.Dy()
	buf := make([]byte, d.bytesPerScanline)
	for y := 0; y < height; y++ {
		if err := d.rleDecode(bufR, buf); err != nil {
			return nil, err
		}
		for x := 0; x < width; x++ {
			v := byte(0)
			for i := 0; i < d.nplanes; i++ {
				v = (v >> 1) | ((buf[d.bytesPerLine*i+(x/8)] << (uint(x) & 7)) & 0x80)
			}
			v >>= uint(8 - d.nplanes)
			img.Pix[y*img.Stride+x] = v
		}
	}
	return img, nil
}

func (d *decoder) rleDecode(bufR *bufio.Reader, out []byte) error {
	for off := 0; off < d.bytesPerScanline; {
		val, err := bufR.ReadByte()
		if err != nil {
			return err
		}
		run := 1
		if val >= 0xc0 {
			run = int(val & 0x3f)
			val, err = bufR.ReadByte()
			if err != nil {
				return err
			}
		}
		for i := 0; i < run; i++ {
			if off >= d.bytesPerScanline {
				return errors.New("pcx: RLE overrun")
			}
			if off < len(out) {
				out[off] = val
			}
			off++
		}
	}
	return nil
}
