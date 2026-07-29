package pcx

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"testing"
)

// rleRoundTrip encodes one scanline with rleEncoder and decodes it again, checking
// that the bytes survive and reporting how long the encoded form was.
func rleRoundTrip(t *testing.T, in []byte) int {
	t.Helper()
	e := newRLEEncoder(len(in))
	for _, b := range in {
		e.put(b)
	}
	encoded := bytes.Clone(e.flush())

	out := make([]byte, len(in))
	if err := rleDecode(bufio.NewReader(bytes.NewReader(encoded)), out, 0); err != nil {
		t.Fatalf("rleDecode: %s", err)
	}
	if !bytes.Equal(in, out) {
		t.Fatalf("round trip changed the data:\n in %v\nout %v", in, out)
	}
	return len(encoded)
}

// TestRLERoundTrip covers the cases where the encoding is easy to get subtly wrong:
// the maximum run length, a byte that looks like a run marker, and the transitions
// between runs and literals.
func TestRLERoundTrip(t *testing.T) {
	t.Parallel()

	repeat := func(b byte, n int) []byte { return bytes.Repeat([]byte{b}, n) }

	for _, tc := range []struct {
		name string
		in   []byte
		// wantLen is the expected encoded length where it is the point of the case;
		// 0 means only the round trip matters.
		wantLen int
	}{
		{name: "single byte", in: []byte{7}, wantLen: 1},
		{name: "two bytes", in: []byte{7, 8}, wantLen: 2},
		// A run marker is 0xc0 plus a 6-bit count, so 63 is the longest single run.
		{name: "run of 63", in: repeat(9, 63), wantLen: 2},
		// One more has to split rather than wrapping the count to 0: a full run of
		// 63 plus a single literal.
		{name: "run of 64", in: repeat(9, 64), wantLen: 3},
		{name: "run of 127", in: repeat(9, 127), wantLen: 5},
		// A literal at or above 0xc0 would be read back as a run marker, so it has
		// to be encoded as a run of one: two bytes for one pixel.
		{name: "literal 0xc0", in: []byte{0xc0}, wantLen: 2},
		{name: "literal 0xff", in: []byte{0xff}, wantLen: 2},
		{name: "high literals", in: []byte{0xc0, 0xc1, 0xff}, wantLen: 6},
		// Below 0xc0 a lone byte stays a literal.
		{name: "literal 0xbf", in: []byte{0xbf}, wantLen: 1},
		{name: "run then literal", in: []byte{1, 1, 1, 2}, wantLen: 3},
		{name: "literal then run", in: []byte{2, 1, 1, 1}, wantLen: 3},
		{name: "alternating", in: []byte{1, 2, 1, 2, 1, 2}, wantLen: 6},
		{name: "run of high bytes", in: repeat(0xc5, 40), wantLen: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			n := rleRoundTrip(t, tc.in)
			if tc.wantLen != 0 && n != tc.wantLen {
				t.Errorf("encoded %d bytes into %d, expected %d", len(tc.in), n, tc.wantLen)
			}
		})
	}
}

// TestRLERoundTripScanlines covers longer, more varied scanlines, including the
// pattern where a run is broken by exactly one differing byte.
func TestRLERoundTripScanlines(t *testing.T) {
	t.Parallel()

	for _, n := range []int{1, 2, 63, 64, 65, 128, 255, 256} {
		in := make([]byte, n)
		for i := range in {
			switch {
			case i%17 == 0:
				in[i] = 0xc0 | byte(i%64)
			case i%5 == 0:
				in[i] = byte(i)
			default:
				in[i] = 0x42
			}
		}
		rleRoundTrip(t, in)
	}
}

// TestRLEDecodePadding covers the scanline padding rleDecode discards. bytesPerLine
// can exceed the pixel width, and those extra bytes are part of the RLE stream but
// not part of the image.
func TestRLEDecodePadding(t *testing.T) {
	t.Parallel()

	// Four bytes of scanline, of which one is padding.
	encoded := []byte{1, 2, 3, 9}
	out := make([]byte, 3)
	if err := rleDecode(bufio.NewReader(bytes.NewReader(encoded)), out, 1); err != nil {
		t.Fatalf("rleDecode: %s", err)
	}
	if want := []byte{1, 2, 3}; !bytes.Equal(out, want) {
		t.Errorf("decoded %v, expected %v", out, want)
	}

	// A run may span the boundary between the pixels and the padding, in which case
	// only the part that lands inside the image is stored.
	encoded = []byte{0xc4, 7}
	out = make([]byte, 3)
	if err := rleDecode(bufio.NewReader(bytes.NewReader(encoded)), out, 1); err != nil {
		t.Fatalf("rleDecode: %s", err)
	}
	if want := []byte{7, 7, 7}; !bytes.Equal(out, want) {
		t.Errorf("decoded %v, expected %v", out, want)
	}
}

// TestRLEDecodeOverrun covers a run that would decode past the end of the scanline.
// Runs never cross a scanline boundary in PCX, so that is a malformed file rather
// than something to silently clamp — clamping is what used to let a decoder and its
// caller disagree about the row length unnoticed.
func TestRLEDecodeOverrun(t *testing.T) {
	t.Parallel()

	var format FormatError
	// A run of 5 into a 3 byte scanline with no padding.
	err := rleDecode(bufio.NewReader(bytes.NewReader([]byte{0xc5, 7})), make([]byte, 3), 0)
	if !errors.As(err, &format) {
		t.Errorf("err is %v (%T), expected a FormatError", err, err)
	}
	// The same overrun counted against the padding as well as the pixels.
	err = rleDecode(bufio.NewReader(bytes.NewReader([]byte{0xc5, 7})), make([]byte, 2), 2)
	if !errors.As(err, &format) {
		t.Errorf("err is %v (%T), expected a FormatError", err, err)
	}
}

// TestRLEDecodeTruncated covers a scanline that runs out of input, in both the run
// marker and the run value position.
func TestRLEDecodeTruncated(t *testing.T) {
	t.Parallel()

	for _, in := range [][]byte{
		{},                 // nothing at all
		{1, 2},             // fewer literals than the scanline needs
		{0xc3},             // a run marker with no value after it
		{0xc2, 1},          // a complete run that still leaves the scanline short
		{0xc0, 1},          // a zero length run, which fills nothing
		{0xc0, 1, 0xc0, 2}, // several of them
	} {
		err := rleDecode(bufio.NewReader(bytes.NewReader(in)), make([]byte, 4), 0)
		if !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Errorf("%v: err is %v (%T), expected io.ErrUnexpectedEOF", in, err, err)
		}
		if errors.Is(err, io.EOF) {
			t.Errorf("%v: err is io.EOF, which reads as a clean end of stream", in)
		}
	}
}

// TestRLEDecodeZeroLengthRun documents what a 0xc0 marker does: it names a run of
// zero bytes, so it consumes two bytes of input and emits nothing. No encoder writes
// one, and the loop still terminates because the scanline can only be filled by
// bytes that do arrive — a stream of nothing but zero length runs ends in
// io.ErrUnexpectedEOF, which TestRLEDecodeTruncated covers.
func TestRLEDecodeZeroLengthRun(t *testing.T) {
	t.Parallel()

	out := make([]byte, 2)
	// A zero length run between two literals contributes nothing to the scanline.
	err := rleDecode(bufio.NewReader(bytes.NewReader([]byte{0xc0, 0xaa, 1, 2})), out, 0)
	if err != nil {
		t.Fatalf("rleDecode: %s", err)
	}
	if want := []byte{1, 2}; !bytes.Equal(out, want) {
		t.Errorf("decoded %v, expected %v", out, want)
	}
}

// TestRLEEncoderReuse covers the reset flush performs. The encoder is kept across
// every row of an image, and a row's pending run must not leak into the next one.
func TestRLEEncoderReuse(t *testing.T) {
	t.Parallel()

	e := newRLEEncoder(4)
	for _, b := range []byte{1, 1, 1, 1} {
		e.put(b)
	}
	first := bytes.Clone(e.flush())
	for _, b := range []byte{2, 3, 4, 5} {
		e.put(b)
	}
	second := bytes.Clone(e.flush())

	if want := []byte{0xc4, 1}; !bytes.Equal(first, want) {
		t.Errorf("first row encoded to %v, expected %v", first, want)
	}
	if want := []byte{2, 3, 4, 5}; !bytes.Equal(second, want) {
		t.Errorf("second row encoded to %v, expected %v", second, want)
	}
	// A flush with nothing pending is empty rather than repeating the last byte.
	if n := len(e.flush()); n != 0 {
		t.Errorf("flush with nothing pending returned %d bytes", n)
	}
}
