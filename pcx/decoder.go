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
	grayscale        bool
	pb4              bool
	colorModel       color.Model
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
		if errors.Is(err, io.EOF) {
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
	d.bounds = image.Rect(
		int(buf[4])|(int(buf[5])<<8),
		int(buf[6])|(int(buf[7])<<8),
		int(buf[8])|(int(buf[9])<<8)+1,
		int(buf[10])|(int(buf[11])<<8)+1)
	d.horizDPI = int(buf[12]) | (int(buf[13]) << 8)
	d.vertDPI = int(buf[14]) | (int(buf[15]) << 8)
	copy(d.colormap[:48], buf[16:16+48])
	// Byte 64 is reserved and should be set to 0
	d.nplanes = int(buf[65])
	// The number of bytes per line MUST be an even number.
	d.bytesPerLine = int(buf[66]) | (int(buf[67]) << 8)
	d.bytesPerScanline = d.bytesPerLine * d.nplanes
	// grayscale is generally ignored entirely. It may just be for display and doesn't change the encoding.
	// I have yet to see a file in the wild that has this set to grayscale.
	d.grayscale = d.version < 5 && buf[68] == 2 // Grayscale flag ignored by PB IV/ IV +
	d.pb4 = buf[68] != 0
	// horizontal and vertical screen size in pixels are found only in PB IV/IV Plus
	d.horizScreenSize = int(buf[70]) | (int(buf[71]) << 8)
	d.vertScreenSize = int(buf[72]) | (int(buf[73]) << 8)
	// The rest of the 54 bytes should be all set to 0

	if d.bytesPerScanline < (d.bounds.Dx()*d.bpp*d.nplanes+7)/8 {
		return FormatError("corrupt image")
	}

	if d.grayscale {
		d.colorModel = color.GrayModel
	} else {
		d.colorModel = color.RGBAModel
	}

	if d.bounds.Dx() <= 0 || d.bounds.Dy() <= 0 {
		return FormatError(fmt.Sprintf("invalid image dimensions %d x %d", d.bounds.Dx(), d.bounds.Dy()))
	}

	return nil
}

func (d *decoder) decode() (image.Image, error) {
	if !d.rle {
		return nil, UnsupportedError("non-RLE")
	}

	bufR := bufio.NewReader(d.r)

	switch {
	case d.colorModel == color.GrayModel:
		if d.bpp == 8 {
			return d.decodeGrayscale(bufR)
		}
		return nil, UnsupportedError("grayscale only supported with 8bpp")
	case d.nplanes == 1:
		if d.bpp == 8 {
			return d.decodeRGBPaletted(bufR)
		}
		return d.decodePaletted(bufR)
	case d.bpp == 8 && (d.nplanes == 3 || d.nplanes == 4):
		return d.decodeRGB(bufR)
	case d.bpp == 1 && (d.nplanes >= 2 && d.nplanes <= 4):
		return d.decodePlanar(bufR)
	}

	return nil, UnsupportedError(fmt.Sprintf("version %d with %d planes %d bpp", d.version, d.nplanes, d.bpp))
}

func (d *decoder) decodeGrayscale(bufR *bufio.Reader) (image.Image, error) {
	img := image.NewGray(d.bounds)
	height := d.bounds.Dy()
	for y := range height {
		if err := rleDecode(bufR, img.Pix[y*img.Stride:], d.bytesPerScanline); err != nil {
			return img, err
		}
	}
	return img, nil
}

func (d *decoder) decodeRGB(bufR *bufio.Reader) (image.Image, error) {
	img := image.NewRGBA(d.bounds)
	width := d.bounds.Dx()
	height := d.bounds.Dy()
	offset := 0
	buf := make([]byte, d.bytesPerScanline)
	for range height {
		if err := rleDecode(bufR, buf, d.bytesPerScanline); err != nil {
			return img, err
		}
		for x := range width {
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

func (d *decoder) decodeRGBPaletted(bufR *bufio.Reader) (image.Image, error) {
	pal := make([]color.Color, 256)
	img := image.NewPaletted(d.bounds, pal)
	height := d.bounds.Dy()
	for y := range height {
		if err := rleDecode(bufR, img.Pix[y*img.Stride:], d.bytesPerScanline); err != nil {
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
	for i := range 256 {
		pal[i] = color.RGBA{R: palBytes[i*3], G: palBytes[i*3+1], B: palBytes[i*3+2], A: 255}
	}

	return img, nil
}

func (d *decoder) decodePaletted(bufR *bufio.Reader) (image.Image, error) {
	pal := make([]color.Color, 1<<uint(d.bpp))
	switch {
	case d.bpp == 1: // B&W
		pal[0] = color.Black
		pal[1] = color.White
	case d.bpp == 2 && d.bounds.Dx() == 320 && d.bounds.Dy() == 200: // CGA
		pal[0] = cga16ColorPalette[d.colormap[0]>>4]
		idx := int(d.colormap[3] >> 5)
		if d.pb4 {
			isReallyPB4 := true
			for _, v := range d.colormap[:12] {
				if v != 0 && v != 255 {
					isReallyPB4 = false
					break
				}
			}
			if isReallyPB4 {
				// PC Paintbush 4.0 encodes the CGA palettes differently than 3.0.
				// Very thankful for the person that figured it out here:
				// https://github.com/wjaguar/mtPaint/blob/master/src/png.c
				// Also described in https://moddingwiki.shikadi.net/wiki/PCX_Format#Newer_method
				// G = colormap[4]
				// B = colormap[5]
				// Pick green palette if G>B in slot 1
				// Pick bright palette if max(G,B) > 200
				idx = 0
				if d.colormap[5] >= d.colormap[4] {
					idx |= 2
				}
				if max(d.colormap[5], d.colormap[4]) > 200 {
					idx |= 1
				}
			}
		}
		copy(pal[1:], cga4ColorPalettes[idx])
	default: // EGA
		if len(pal)*3 > len(d.colormap) {
			return nil, fmt.Errorf("palette larger (%d) than available colormap (%d)", len(pal)*3, len(d.colormap))
		}
		for i := 0; i < len(pal)*3; i += 3 {
			pal[i/3] = color.RGBA{R: d.colormap[i], G: d.colormap[i+1], B: d.colormap[i+2], A: 255}
		}
	}

	img := image.NewPaletted(d.bounds, pal)
	width, height := d.bounds.Dx(), d.bounds.Dy()
	buf := make([]byte, d.bytesPerScanline)
	mask := byte((1 << uint(d.bpp)) - 1)
	for y := range height {
		if err := rleDecode(bufR, buf, d.bytesPerScanline); err != nil {
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

func (d *decoder) decodePlanar(bufR *bufio.Reader) (image.Image, error) {
	pal := make([]color.Color, 1<<uint(d.nplanes))
	for i := 0; i < len(pal)*3; i += 3 {
		pal[i/3] = color.RGBA{R: d.colormap[i], G: d.colormap[i+1], B: d.colormap[i+2], A: 255}
	}
	img := image.NewPaletted(d.bounds, pal)

	width := d.bounds.Dx()
	height := d.bounds.Dy()
	buf := make([]byte, d.bytesPerScanline)
	for y := range height {
		if err := rleDecode(bufR, buf, d.bytesPerScanline); err != nil {
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
