package pcx

import (
	"image"
	"image/color"
	"io"
)

// Encode writes the Image m to w in PCX format.
func Encode(w io.Writer, m image.Image) error {
	em, ok := m.(*Image)
	if ok {
		m = em.Image
	}
	switch im := m.(type) {
	case *image.RGBA:
		return encodeRGBA(w, im, em)
	case *image.Paletted:
		return encodePaletted(w, im, em)
	case image.PalettedImage:
		cm := im.ColorModel()
		if p, ok := cm.(color.Palette); ok {
			return encodePalettedImage(w, im, p, em)
		}
	}
	return encodeGeneric(w, m, em)
}

func encodeGeneric(w io.Writer, m image.Image, em *Image) error {
	b := m.Bounds()
	odd := b.Dx() & 1
	bytesPerLine := b.Dx() + odd
	if err := writeHeader(w, 8, 3, bytesPerLine, b, nil, em); err != nil {
		return err
	}
	rline := newRLEEncoder(bytesPerLine)
	gline := newRLEEncoder(bytesPerLine)
	bline := newRLEEncoder(bytesPerLine)
	for y := b.Min.Y; y < b.Max.Y; y++ {
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

func encodeRGBA(w io.Writer, m *image.RGBA, em *Image) error {
	b := m.Bounds()
	odd := b.Dx() & 1
	bytesPerLine := b.Dx() + odd
	if err := writeHeader(w, 8, 3, bytesPerLine, b, nil, em); err != nil {
		return err
	}
	width := b.Dx()
	height := b.Dy()
	rline := newRLEEncoder(bytesPerLine)
	gline := newRLEEncoder(bytesPerLine)
	bline := newRLEEncoder(bytesPerLine)
	for y := range height {
		o := y * m.Stride
		for range width {
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

func encodePaletted(w io.Writer, m *image.Paletted, em *Image) error {
	if len(m.Palette) <= 16 {
		return encodePaletted16Colors(w, m, em)
	}

	b := m.Bounds()
	odd := b.Dx() & 1
	bytesPerLine := b.Dx() + odd
	if err := writeHeader(w, 8, 1, bytesPerLine, b, m.Palette, em); err != nil {
		return err
	}
	width := b.Dx()
	height := b.Dy()
	line := newRLEEncoder(bytesPerLine)
	for y := range height {
		o := y * m.Stride
		for range width {
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

func encodePaletted16Colors(w io.Writer, m *image.Paletted, em *Image) error {
	b := m.Bounds()
	if len(m.Palette) == 4 && b.Dx() == 320 && b.Dy() == 200 && isCGAPalette(m.Palette) {
		cgaIndexes := make([]int, 4)
		for i, c := range m.Palette {
			cgaIndexes[i] = cgaPaletteIndex(c)
		}
		palette := make([]color.Color, len(m.Palette))
		for i := range palette {
			palette[i] = color.RGBA{
				R: uint8(255 * ((cgaIndexes[i] >> 2) & 1)),
				G: uint8(255 * ((cgaIndexes[i] >> 1) & 1)),
				B: uint8(255 * (cgaIndexes[i] & 1)),
			}
		}
		firstColIx := cgaPaletteIndex(m.Palette[0])
		palette[0] = color.RGBA{R: uint8(firstColIx << 4)}
		return encodePalettedSinglePlane(w, m, em, 2, palette)
	}
	switch len(m.Palette) {
	case 0, 1, 2:
		return encodePalettedSinglePlane(w, m, em, 1, []color.Color{color.RGBA{R: 0xff}})
	case 4:
		return encodePalettedSinglePlane(w, m, em, 2, m.Palette)
	}

	bpp := 1
	nPlanes := 4
	if len(m.Palette) == 8 {
		nPlanes = 3
	}
	odd := b.Dx() & 1
	bytesPerLine := (b.Dx() + odd + 7) / 8
	if err := writeHeader(w, bpp, nPlanes, bytesPerLine, b, m.Palette, em); err != nil {
		return err
	}
	width := b.Dx()
	height := b.Dy()
	line := newRLEEncoder(bytesPerLine)
	for y := range height {
		o := y * m.Stride
		for i := range nPlanes {
			var v byte
			var n int
			for x := range width {
				v <<= 1
				v |= (m.Pix[o+x] >> i) & 1
				if x&7 == 7 {
					line.put(v)
					n++
					v = 0
				}
			}
			v <<= 8 - (width & 7)
			for n < bytesPerLine {
				line.put(v)
				v = 0
				n++
			}
		}
		if _, err := w.Write(line.flush()); err != nil {
			return err
		}
	}
	return nil
}

func encodePalettedSinglePlane(w io.Writer, m *image.Paletted, em *Image, bpp int, palette color.Palette) error {
	b := m.Bounds()
	pixelsPerByte := 8 / bpp
	bytesPerLine := (b.Dx() + pixelsPerByte - 1) / pixelsPerByte
	// The number of bytes per line must be even.
	extraBytes := bytesPerLine & 1
	bytesPerLine += extraBytes
	if err := writeHeader(w, bpp, 1, bytesPerLine, b, palette, em); err != nil {
		return err
	}
	width := b.Dx()
	height := b.Dy()
	line := newRLEEncoder(bytesPerLine)
	mask := (1 << bpp) - 1
	xmask := (8 / bpp) - 1
	shift := bpp * (8 - width&xmask) & 7
	for y := range height {
		o := y * m.Stride
		var v byte
		for x := range width {
			v <<= bpp
			v |= m.Pix[o+x] & byte(mask)
			if x&xmask == xmask {
				line.put(v)
				v = 0
			}
		}
		if width&xmask != 0 {
			// The value needs to start from the most significant bits.
			v <<= shift
			line.put(v)
		}
		// The output length needs to be the same as bytesPerLine so include any extra bytes due to alignment.
		for range extraBytes {
			// Outputting same as last byte since it'll lead to the shortest output. Is there any reason this needs to be 0?
			line.put(v)
		}
		if _, err := w.Write(line.flush()); err != nil {
			return err
		}
	}
	return nil
}

func encodePalettedImage(w io.Writer, m image.PalettedImage, p color.Palette, em *Image) error {
	// TODO: implement all the functionality in encodePaletted
	b := m.Bounds()
	odd := b.Dx() & 1
	bytesPerLine := b.Dx() + odd
	if err := writeHeader(w, 8, 1, bytesPerLine, b, p, em); err != nil {
		return err
	}
	line := newRLEEncoder(bytesPerLine)
	for y := b.Min.Y; y < b.Max.Y; y++ {
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

func writeHeader(w io.Writer, bpp, nplanes, bytesPerLine int, bounds image.Rectangle, egaPalette color.Palette, em *Image) error {
	buf := make([]byte, 128)
	buf[0] = magic
	if em != nil && em.Version >= 0 && em.Version <= 5 {
		buf[1] = byte(em.Version)
	} else {
		buf[1] = 5
	}
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
	if em != nil {
		buf[12] = byte(em.HorizDPI)
		buf[13] = byte(em.HorizDPI >> 8)
		buf[14] = byte(em.VertDPI)
		buf[15] = byte(em.VertDPI >> 8)
	}
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
	// PB4/Color/Gray
	if em == nil || em.Paintbrush4 {
		buf[68] = 1
	} else {
		buf[68] = 0
	}
	if em != nil {
		buf[70] = byte(em.HorizScreenSize)
		buf[71] = byte(em.HorizScreenSize >> 8)
		buf[72] = byte(em.VertScreenSize)
		buf[73] = byte(em.VertScreenSize >> 8)
	}
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
