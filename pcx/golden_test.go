package pcx

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const goldenPath = "testdata/golden.txt"

const goldenHeader = `# Golden decode records: the decoded output every fixture must keep producing.
#
# Regenerate with ` + "`go test ./pcx/ -update`" + `. A diff here means decoded output changed:
# judge it by eye with ` + "`go test ./pcx/ -save`" + ` before committing. See golden-pcx.txt for
# how the values were verified in the first place.
#
# name  type  bounds  stride  palette(count/hash)  pixels(hash)
`

// goldenRecord is a canonical, diffable summary of one decoded fixture. The metadata
// stays in plain text so a failing diff is readable on its own; only the bulk pixel
// data is hashed.
type goldenRecord struct {
	name    string
	typ     string
	bounds  string
	stride  string
	palette string
	pixels  string
}

func (r goldenRecord) String() string {
	return fmt.Sprintf("%-18s %-16s %-18s %-11s %-16s %s",
		r.name, r.typ, r.bounds, r.stride, r.palette, r.pixels)
}

// fields returns the comparable parts of the record, labelled so a mismatch can name
// what moved rather than just reporting that two lines differ.
func (r goldenRecord) fields() []struct{ label, value string } {
	return []struct{ label, value string }{
		{"type", r.typ},
		{"bounds", r.bounds},
		{"stride", r.stride},
		{"palette", r.palette},
		{"pixel data", r.pixels},
	}
}

func shortHash(b []byte, n int) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:n])
}

// recordFor summarises a decoded image. Pixel bytes and palette are hashed separately
// so that a palette-only regression — the shape of the CGA bug this package has had
// before — is distinguishable from a change in the pixel data.
func recordFor(name string, img image.Image) goldenRecord {
	r := goldenRecord{
		name:    name,
		typ:     fmt.Sprintf("%T", img),
		bounds:  img.Bounds().String(),
		palette: "-",
	}
	var pix []byte
	switch m := img.(type) {
	case *image.Paletted:
		pix = m.Pix
		r.stride = fmt.Sprintf("stride=%d", m.Stride)
		var buf bytes.Buffer
		for _, c := range m.Palette {
			cr, cg, cb, ca := c.RGBA()
			fmt.Fprintf(&buf, "%04x%04x%04x%04x", cr, cg, cb, ca)
		}
		r.palette = fmt.Sprintf("pal=%d/%s", len(m.Palette), shortHash(buf.Bytes(), 4))
	case *image.Gray:
		pix, r.stride = m.Pix, fmt.Sprintf("stride=%d", m.Stride)
	case *image.RGBA:
		pix, r.stride = m.Pix, fmt.Sprintf("stride=%d", m.Stride)
	case *image.NRGBA:
		pix, r.stride = m.Pix, fmt.Sprintf("stride=%d", m.Stride)
	default:
		// A new decoder output type must be added here deliberately: falling back to
		// At() would hash a lossy view of the image and hide the type change.
		panic(fmt.Sprintf("golden records do not handle %T", img))
	}
	r.pixels = "pix=" + shortHash(pix, 8)
	return r
}

// loadGolden parses testdata/golden.txt, keyed by fixture name.
func loadGolden(t *testing.T) map[string]goldenRecord {
	t.Helper()
	b, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("%s (run `go test ./pcx/ -update` to create it): %s", goldenPath, err)
	}
	records := make(map[string]goldenRecord)
	for i, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		if len(f) != 6 {
			t.Fatalf("%s:%d: expected 6 fields, got %d: %q", goldenPath, i+1, len(f), line)
		}
		records[f[0]] = goldenRecord{
			name: f[0], typ: f[1], bounds: f[2], stride: f[3], palette: f[4], pixels: f[5],
		}
	}
	return records
}

func writeGolden(records []goldenRecord) error {
	var buf bytes.Buffer
	buf.WriteString(goldenHeader)
	for _, r := range records {
		buf.WriteString(strings.TrimRight(r.String(), " "))
		buf.WriteByte('\n')
	}
	return os.WriteFile(goldenPath, buf.Bytes(), 0o644)
}

// TestDecodeGolden pins the decoded output of every fixture. It decodes through the
// exported Decode, so it covers what a caller of this package actually gets.
func TestDecodeGolden(t *testing.T) {
	t.Parallel()

	paths, err := filepath.Glob("testdata/*.pcx")
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(paths)

	var want map[string]goldenRecord
	if !*flagUpdate {
		want = loadGolden(t)
	}

	records := make([]goldenRecord, 0, len(paths))
	for _, path := range paths {
		name := filepath.Base(path)
		if strings.HasPrefix(name, ".") {
			continue
		}
		t.Run(name, func(t *testing.T) {
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			img, err := Decode(bytes.NewReader(b))
			if err != nil {
				t.Fatalf("failed to decode: %s", err)
			}
			got := recordFor(name, img)
			records = append(records, got)
			if *flagUpdate {
				return
			}
			exp, ok := want[name]
			if !ok {
				t.Fatalf("no golden record; a new fixture must say which decode path it "+
					"covers, then `go test ./pcx/ -update` to add:\n  %s", got)
			}
			delete(want, name)
			for i, f := range got.fields() {
				if e := exp.fields()[i]; f.value != e.value {
					t.Errorf("%s changed: got %s, want %s\n  got:  %s\n  want: %s\n"+
						"Inspect with `go test ./pcx/ -save` before regenerating.",
						f.label, f.value, e.value, got, exp)
				}
			}
		})
	}

	if *flagUpdate {
		if err := writeGolden(records); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %d records to %s", len(records), goldenPath)
		return
	}
	// Anything left is a record whose fixture is gone, which would otherwise let the
	// file drift out of sync with testdata/ unnoticed.
	for name := range want {
		t.Errorf("golden record for %s has no fixture; rerun with -update", name)
	}
}
