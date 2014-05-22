package pcx

import (
	"image"
	"image/color"
	"io"
)

func Encode(w io.Writer, m image.Image) error {
	switch im := m.(type) {
	case *image.RGBA:
		return encodeRGBA(w, im)
	case *image.Paletted:
		return encodePaletted(w, im)
	case image.PalettedImage:
		cm := im.ColorModel()
		if p, ok := cm.(color.Palette); ok {
			return encodePalettedImage(w, im, p)
		}
	}
	return encodeGeneric(w, m)
}

func encodeGeneric(w io.Writer, m image.Image) error {
	b := m.Bounds()
	odd := b.Dx() & 1
	bytesPerLine := b.Dx() + odd
	if err := writeHeader(w, 8, 3, bytesPerLine, b, nil); err != nil {
		return err
	}
	rline := &rleBuffer{b: make([]byte, b.Dx())}
	gline := &rleBuffer{b: make([]byte, b.Dx())}
	bline := &rleBuffer{b: make([]byte, b.Dx())}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		rline.reset()
		gline.reset()
		bline.reset()
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, b, _ := m.At(x, y).RGBA()
			rline.put(byte(r >> 8))
			gline.put(byte(g >> 8))
			bline.put(byte(b >> 8))
		}
		if odd != 0 {
			rline.put(0)
			gline.put(0)
			bline.put(0)
		}
		if _, err := w.Write(rline.flush()); err != nil {
			return err
		}
		if _, err := w.Write(gline.flush()); err != nil {
			return err
		}
		if _, err := w.Write(bline.flush()); err != nil {
			return err
		}
	}
	return nil
}

func encodeRGBA(w io.Writer, m *image.RGBA) error {
	b := m.Bounds()
	odd := b.Dx() & 1
	bytesPerLine := b.Dx() + odd
	if err := writeHeader(w, 8, 3, bytesPerLine, b, nil); err != nil {
		return err
	}
	width := b.Dx()
	height := b.Dy()
	rline := &rleBuffer{b: make([]byte, width)}
	gline := &rleBuffer{b: make([]byte, width)}
	bline := &rleBuffer{b: make([]byte, width)}
	for y := 0; y < height; y++ {
		rline.reset()
		gline.reset()
		bline.reset()
		o := y * m.Stride
		for x := 0; x < width; x++ {
			rline.put(m.Pix[o])
			gline.put(m.Pix[o+1])
			bline.put(m.Pix[o+2])
			o += 4
		}
		if odd != 0 {
			rline.put(0)
			gline.put(0)
			bline.put(0)
		}
		if _, err := w.Write(rline.flush()); err != nil {
			return err
		}
		if _, err := w.Write(gline.flush()); err != nil {
			return err
		}
		if _, err := w.Write(bline.flush()); err != nil {
			return err
		}
	}
	return nil
}

func encodePaletted(w io.Writer, m *image.Paletted) error {
	b := m.Bounds()
	odd := b.Dx() & 1
	bytesPerLine := b.Dx() + odd
	if err := writeHeader(w, 8, 1, bytesPerLine, b, nil); err != nil {
		return err
	}
	width := b.Dx()
	height := b.Dy()
	line := &rleBuffer{b: make([]byte, width)}
	for y := 0; y < height; y++ {
		line.reset()
		o := y * m.Stride
		for x := 0; x < width; x++ {
			line.put(m.Pix[o])
			o++
		}
		if odd != 0 {
			line.put(0)
		}
		if _, err := w.Write(line.flush()); err != nil {
			return err
		}
	}
	return writeExtendedPalette(w, m.Palette)
}

func encodePalettedImage(w io.Writer, m image.PalettedImage, p color.Palette) error {
	b := m.Bounds()
	odd := b.Dx() & 1
	bytesPerLine := b.Dx() + odd
	if err := writeHeader(w, 8, 1, bytesPerLine, b, nil); err != nil {
		return err
	}
	line := &rleBuffer{b: make([]byte, b.Dx())}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		line.reset()
		for x := b.Min.X; x < b.Max.X; x++ {
			line.put(m.ColorIndexAt(x, y))
		}
		if odd != 0 {
			line.put(0)
		}
		if _, err := w.Write(line.flush()); err != nil {
			return err
		}
	}
	return writeExtendedPalette(w, p)
}

func writeHeader(w io.Writer, bpp, nplanes, bytesPerLine int, bounds image.Rectangle, egaPalette color.Palette) error {
	buf := make([]byte, 128)
	buf[0] = magic
	buf[1] = 5 // version
	buf[2] = 1 // RLE
	buf[3] = byte(bpp)
	buf[4] = byte(bounds.Min.X & 0xff)
	buf[5] = byte(bounds.Min.X >> 8)
	buf[6] = byte(bounds.Min.Y & 0xff)
	buf[7] = byte(bounds.Min.Y >> 8)
	buf[8] = byte((bounds.Max.X - 1) & 0xff)
	buf[9] = byte((bounds.Max.X - 1) >> 8)
	buf[10] = byte((bounds.Max.Y - 1) & 0xff)
	buf[11] = byte((bounds.Max.Y - 1) >> 8)
	if len(egaPalette) > 16 {
		egaPalette = egaPalette[:16]
	}
	for i, c := range egaPalette {
		r, g, b, _ := c.RGBA()
		buf[16+0+i*3] = byte(r >> 8)
		buf[16+1+i*3] = byte(g >> 8)
		buf[16+2+i*3] = byte(b >> 8)
	}
	buf[65] = byte(nplanes)
	buf[66] = byte(bytesPerLine & 0xff)
	buf[67] = byte(bytesPerLine >> 8)
	buf[68] = 1 // Color/BW
	_, err := w.Write(buf)
	return err
}

func writeExtendedPalette(w io.Writer, palette color.Palette) error {
	buf := make([]byte, 3*256+1)
	buf[0] = paletteMagic
	for i, c := range palette {
		r, g, b, _ := c.RGBA()
		buf[1+i*3] = byte(r >> 8)
		buf[2+i*3] = byte(g >> 8)
		buf[3+i*3] = byte(b >> 8)
	}
	_, err := w.Write(buf)
	return err
}

type rleBuffer struct {
	b []byte
	n int
	c byte
}

func (r *rleBuffer) put(b byte) {
	if r.n == 0 {
		r.c = b
		r.n = 1
	} else if r.n != 0 {
		if b == r.c && r.n != 63 {
			r.n++
			return
		}
		if r.n != 1 || r.c >= 0xc0 {
			r.b = append(r.b, 0xc0|byte(r.n))
		}
		r.b = append(r.b, r.c)
		r.c = b
		r.n = 1
	}
}

func (r *rleBuffer) flush() []byte {
	if r.n != 0 {
		if r.n != 1 || r.c >= 0xc0 {
			r.b = append(r.b, 0xc0|byte(r.n))
		}
		r.b = append(r.b, r.c)
	}
	r.n = 0
	return r.b
}

func (r *rleBuffer) reset() {
	r.b = r.b[:0]
}
