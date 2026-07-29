package pcx

import (
	"bufio"
	"errors"
)

type rleEncoder struct {
	buf  []byte
	run  int
	last byte
}

func newRLEEncoder(size int) *rleEncoder {
	return &rleEncoder{
		buf: make([]byte, 0, size),
	}
}

func (r *rleEncoder) put(b byte) {
	if r.run != 0 {
		// If the new byte is the same as the previous and the run isn't yet too long then extend the current run.
		if b == r.last && r.run != 0x3f {
			r.run++
			return
		}
		// If there's already a run of length > 1 or the last byte was >=C0 (run signifier) then output a run byte.
		if r.run != 1 || r.last >= 0xc0 {
			r.buf = append(r.buf, 0xc0|byte(r.run))
		}
		r.buf = append(r.buf, r.last)
	}
	r.last = b
	r.run = 1
}

func (r *rleEncoder) flush() []byte {
	if r.run != 0 {
		if r.run != 1 || r.last >= 0xc0 {
			r.buf = append(r.buf, 0xc0|byte(r.run))
		}
		r.buf = append(r.buf, r.last)
	}
	b := r.buf
	r.run = 0
	r.buf = r.buf[:0]
	return b
}

// rleDecode decodes one scanline of len(out)+pad bytes of RLE data from bufR,
// storing the first len(out) of them in out and discarding the remaining pad
// bytes of scanline padding. Runs never cross a scanline boundary, so a run
// that would decode past the end of the scanline is an error rather than
// something to clamp.
func rleDecode(bufR *bufio.Reader, out []byte, pad int) error {
	total := len(out) + pad
	for off := 0; off < total; {
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
		for range run {
			if off >= total {
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
