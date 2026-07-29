package pcx

import (
	"fmt"
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
	case *image.Gray:
		return encodeGray(w, im, em)
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
	if err := writeHeader(w, 8, 3, bytesPerLine, b, nil, em, false); err != nil {
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
	if err := writeHeader(w, 8, 3, bytesPerLine, b, nil, em, false); err != nil {
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

// encodeGray writes m as the 8bpp single plane variant with no extended palette,
// marked grayscale by the palette info byte. That is the same layout an 8bpp
// paletted image uses, so the missing palette is what makes it grayscale.
func encodeGray(w io.Writer, m *image.Gray, em *Image) error {
	b := m.Bounds()
	odd := b.Dx() & 1
	bytesPerLine := b.Dx() + odd
	if err := writeHeader(w, 8, 1, bytesPerLine, b, nil, em, true); err != nil {
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
	return nil
}

func encodePaletted(w io.Writer, m *image.Paletted, em *Image) error {
	if len(m.Palette) <= 16 {
		return encodePaletted16Colors(w, m, em)
	}

	b := m.Bounds()
	odd := b.Dx() & 1
	bytesPerLine := b.Dx() + odd
	if err := writeHeader(w, 8, 1, bytesPerLine, b, m.Palette, em, false); err != nil {
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
		// Write the PC Paintbrush 4.0 form: the background as a colour index in the high
		// nibble of the first entry's red byte, and the true CGA RGB for the remaining
		// entries. The intensity has to survive in the values themselves, since the
		// decoder recovers it from entry 1 being brighter than 200 — expanding the index
		// bits to 0/255 instead would turn every dim palette into its bright twin. The
		// colour burst palettes can't be expressed in this form at all.
		palette := make([]color.Color, len(m.Palette))
		for i, c := range m.Palette {
			palette[i] = cga16ColorPalette[cgaPaletteIndex(c)]
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
	if err := writeHeader(w, bpp, nPlanes, bytesPerLine, b, m.Palette, em, false); err != nil {
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
	if err := writeHeader(w, bpp, 1, bytesPerLine, b, palette, em, false); err != nil {
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
	if err := writeHeader(w, 8, 1, bytesPerLine, b, p, em, false); err != nil {
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

func writeHeader(w io.Writer, bpp, nplanes, bytesPerLine int, bounds image.Rectangle, egaPalette color.Palette, em *Image, grayscale bool) error {
	// PCX stores the origin, the inclusive maximum and the bytes per line as
	// unsigned 16-bit values, so anything outside that range would silently
	// truncate into a file that no reader can make sense of. These report a
	// caller error rather than FormatError/UnsupportedError, which describe a
	// malformed or unimplemented input *file*.
	if bounds.Dx() <= 0 || bounds.Dy() <= 0 {
		return fmt.Errorf("pcx: cannot encode empty image with bounds %v", bounds)
	}
	if bounds.Min.X < 0 || bounds.Min.Y < 0 {
		return fmt.Errorf("pcx: cannot encode image with negative origin %v", bounds.Min)
	}
	if bounds.Max.X-1 > 0xffff || bounds.Max.Y-1 > 0xffff {
		return fmt.Errorf("pcx: image bounds %v exceed the maximum PCX dimensions of 65536x65536", bounds)
	}
	if bytesPerLine > 0xffff {
		return fmt.Errorf("pcx: bytes per line (%d) exceeds the PCX maximum of 65535", bytesPerLine)
	}

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
	switch {
	case grayscale:
		// The decoder uses this, together with the absence of an extended
		// palette, to decide that the image is grayscale.
		buf[68] = paletteInfoGrayscale
	case em == nil || em.Paintbrush4:
		buf[68] = paletteInfoColor
	default:
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
	// An extended palette holds exactly 256 entries. image.Paletted permits more,
	// but Pix is a []uint8 so entries above 255 can never be referenced.
	if len(palette) > 256 {
		palette = palette[:256]
	}
	for i, c := range palette {
		r, g, b, _ := c.RGBA()
		buf[1+i*3] = byte(r >> 8)
		buf[2+i*3] = byte(g >> 8)
		buf[3+i*3] = byte(b >> 8)
	}
	_, err := w.Write(buf)
	return err
}
