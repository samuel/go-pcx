package pcx

import (
	"fmt"
	"image"
	"image/color"
	"io"
)

// Encode writes the image m to w in PCX format. The variant is chosen from m's
// concrete type and contents:
//
//   - *image.Gray: 8 bits per pixel, 1 plane, no palette, marked grayscale
//   - *image.Paletted with more than 16 colors: 8 bits per pixel, 1 plane, with the
//     palette after the pixel data
//   - *image.Paletted with 16 or fewer: the smallest sub-8bpp variant that can still
//     hold the palette's colors — monochrome, CGA, 2 bits per pixel, or 1 bit per
//     pixel across 3 or 4 planes
//   - *image.RGBA, *image.NRGBA and anything else: 8 bits per pixel over 3 planes,
//     or 4 planes when the image is not fully opaque
//
// Three conversions are not exact, and a round trip through Decode shows them:
//
//   - A palette of fewer than 16 colors is written to a variant that holds a power of
//     two of them, so the colors survive but the palette length rounds up. The
//     monochrome variant stores no colors at all and is used only for a palette that
//     is black and white; a CGA color burst palette cannot be written back at all.
//   - PCX stores straight alpha, so a translucent *image.RGBA is un-premultiplied on
//     the way out and decodes as an *image.NRGBA. An opaque *image.NRGBA loses its
//     alpha plane and decodes as an *image.RGBA. Both preserve every value.
//   - Samples wider than 8 bits are truncated to the 8 bits per channel PCX stores.
//
// Passing an *ImageWithHeader steers the header metadata; see ImageWithHeader. An
// image whose bounds PCX cannot represent — empty, negative origin, or beyond 65536
// in either direction — is rejected with a plain error, and nothing is written to w.
func Encode(w io.Writer, m image.Image) error {
	em, ok := m.(*ImageWithHeader)
	if ok {
		m = em.Image
	}
	switch im := m.(type) {
	case *image.RGBA:
		return encodeRGBA(w, im, em)
	case *image.NRGBA:
		return encodeNRGBA(w, im, em)
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

// isOpaque reports whether every pixel of m is fully opaque, which decides whether
// the truecolor paths need a fourth plane for alpha. Everything in image/ implements
// Opaque, so the per-pixel fallback is only for foreign implementations.
func isOpaque(m image.Image) bool {
	if o, ok := m.(interface{ Opaque() bool }); ok {
		return o.Opaque()
	}
	b := m.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if _, _, _, a := m.At(x, y).RGBA(); a != 0xffff {
				return false
			}
		}
	}
	return true
}

// truecolorEncoder writes the 8bpp three or four plane variants one row at a time.
// One RLE encoder per plane, all flushed at the end of every row, since a run may
// never cross a scanline boundary.
type truecolorEncoder struct {
	w     io.Writer
	lines []*rleEncoder
	odd   int
}

// newTruecolorEncoder writes the header for m and returns an encoder for its rows.
// nplanes is 4 when m carries alpha, in which case the fourth plane holds straight
// alpha as PCX stores it.
func newTruecolorEncoder(w io.Writer, m image.Image, em *ImageWithHeader, nplanes int) (*truecolorEncoder, error) {
	b := m.Bounds()
	odd := b.Dx() & 1
	bytesPerLine := b.Dx() + odd
	if err := writeHeader(w, 8, nplanes, bytesPerLine, b, nil, em, false); err != nil {
		return nil, err
	}
	e := &truecolorEncoder{w: w, lines: make([]*rleEncoder, nplanes), odd: odd}
	for i := range e.lines {
		e.lines[i] = newRLEEncoder(bytesPerLine)
	}
	return e, nil
}

// put appends one pixel to the current row. The alpha is ignored when the encoder
// was created for three planes.
func (e *truecolorEncoder) put(r, g, b, a byte) {
	e.lines[0].put(r)
	e.lines[1].put(g)
	e.lines[2].put(b)
	if len(e.lines) == 4 {
		e.lines[3].put(a)
	}
}

// flush pads the current row to bytesPerLine and writes every plane.
func (e *truecolorEncoder) flush() error {
	for _, l := range e.lines {
		if e.odd != 0 {
			l.put(0)
		}
		if _, err := e.w.Write(l.flush()); err != nil {
			return err
		}
	}
	return nil
}

func encodeGeneric(w io.Writer, m image.Image, em *ImageWithHeader) error {
	nplanes := 3
	if !isOpaque(m) {
		nplanes = 4
	}
	e, err := newTruecolorEncoder(w, m, em, nplanes)
	if err != nil {
		return err
	}
	b := m.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			// Color.RGBA returns premultiplied values while PCX stores straight
			// alpha, so converting through color.NRGBA is what keeps a translucent
			// pixel's color from being darkened by its own alpha.
			c := color.NRGBAModel.Convert(m.At(x, y)).(color.NRGBA)
			e.put(c.R, c.G, c.B, c.A)
		}
		if err := e.flush(); err != nil {
			return err
		}
	}
	return nil
}

func encodeRGBA(w io.Writer, m *image.RGBA, em *ImageWithHeader) error {
	if !m.Opaque() {
		// image.RGBA is alpha-premultiplied, so a translucent image has to be
		// un-premultiplied before it can be written. encodeGeneric does that
		// conversion per pixel; the fast path below is only valid while alpha is
		// 255, where the two representations are identical.
		return encodeGeneric(w, m, em)
	}
	e, err := newTruecolorEncoder(w, m, em, 3)
	if err != nil {
		return err
	}
	b := m.Bounds()
	for y := range b.Dy() {
		o := y * m.Stride
		for range b.Dx() {
			e.put(m.Pix[o], m.Pix[o+1], m.Pix[o+2], 255)
			o += 4
		}
		if err := e.flush(); err != nil {
			return err
		}
	}
	return nil
}

// encodeNRGBA writes m as the 8bpp truecolor variant. image.NRGBA already holds
// straight alpha, which is what PCX stores, so its Pix goes out as is. The alpha
// plane is written only when the image needs it, keeping opaque images to the three
// plane form that every PCX reader handles.
func encodeNRGBA(w io.Writer, m *image.NRGBA, em *ImageWithHeader) error {
	nplanes := 3
	if !m.Opaque() {
		nplanes = 4
	}
	e, err := newTruecolorEncoder(w, m, em, nplanes)
	if err != nil {
		return err
	}
	b := m.Bounds()
	for y := range b.Dy() {
		o := y * m.Stride
		for range b.Dx() {
			e.put(m.Pix[o], m.Pix[o+1], m.Pix[o+2], m.Pix[o+3])
			o += 4
		}
		if err := e.flush(); err != nil {
			return err
		}
	}
	return nil
}

// encodeGray writes m as the 8bpp single plane variant with no extended palette,
// marked grayscale by the palette info byte. That is the same layout an 8bpp
// paletted image uses, so the missing palette is what makes it grayscale.
func encodeGray(w io.Writer, m *image.Gray, em *ImageWithHeader) error {
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

func encodePaletted(w io.Writer, m *image.Paletted, em *ImageWithHeader) error {
	if len(m.Palette) <= 16 {
		return encodePaletted16Colors(w, m, em)
	}
	return encodePaletted8bpp(w, m, em)
}

// encodePaletted8bpp writes m as the 8bpp single plane variant with a trailing
// extended palette. It is the only paletted form that stores its colors verbatim, so
// it also serves as the fallback for the small palettes the sub-8bpp variants cannot
// represent faithfully.
func encodePaletted8bpp(w io.Writer, m *image.Paletted, em *ImageWithHeader) error {
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

func encodePaletted16Colors(w io.Writer, m *image.Paletted, em *ImageWithHeader) error {
	b := m.Bounds()
	if len(m.Palette) == 4 && b.Dx() == 320 && b.Dy() == 200 && isCGAPalette(m.Palette) {
		// Write the PC Paintbrush 4.0 form: the background as a color index in the high
		// nibble of the first entry's red byte, and the true CGA RGB for the remaining
		// entries. The intensity has to survive in the values themselves, since the
		// decoder recovers it from entry 1 being brighter than 200 — expanding the index
		// bits to 0/255 instead would turn every dim palette into its bright twin. The
		// color burst palettes can't be expressed in this form at all.
		palette := make([]color.Color, len(m.Palette))
		for i, c := range m.Palette {
			palette[i] = cga16ColorPalette[cgaPaletteIndex(c)]
		}
		firstColIx := cgaPaletteIndex(m.Palette[0])
		palette[0] = color.RGBA{R: uint8(firstColIx << 4)}
		return encodePalettedSinglePlane(w, m, em, 2, palette)
	}
	switch {
	case isBlackAndWhitePalette(m.Palette):
		// The 1bpp variant carries no colors of its own — the decoder always
		// reconstructs black and white — so it may only be used when that is
		// what the palette holds.
		return encodePalettedSinglePlane(w, m, em, 1, m.Palette)
	case len(m.Palette) <= 4:
		if b.Dx() == 320 && b.Dy() == 200 {
			// Any 2bpp 320x200 file is read back as CGA, with its palette
			// rebuilt from the colormap's flag bits and the written colors
			// discarded. That is not resolvable from the header, so a palette
			// that isn't CGA takes the 8bpp form instead, which costs bytes for
			// this one shape but always round-trips.
			return encodePaletted8bpp(w, m, em)
		}
		return encodePalettedSinglePlane(w, m, em, 2, m.Palette)
	}

	// The remaining sizes go out as 1bpp planes, one bit of the index per plane, so
	// the plane count is the number of bits the largest index needs.
	bpp := 1
	nPlanes := 4
	if len(m.Palette) <= 8 {
		nPlanes = 3
	}
	// The number of bytes per line must be even.
	bytesPerLine := (b.Dx() + 7) / 8
	bytesPerLine += bytesPerLine & 1
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

func encodePalettedSinglePlane(w io.Writer, m *image.Paletted, em *ImageWithHeader, bpp int, palette color.Palette) error {
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

func encodePalettedImage(w io.Writer, m image.PalettedImage, p color.Palette, em *ImageWithHeader) error {
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

func writeHeader(w io.Writer, bpp, nplanes, bytesPerLine int, bounds image.Rectangle, egaPalette color.Palette, em *ImageWithHeader, grayscale bool) error {
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
	case em == nil || em.PaletteInfoSet:
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
