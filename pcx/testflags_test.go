package pcx

import "flag"

// Flags shared by the tests.
var (
	// flagSave writes every decoded image to testout/ as a PNG. TestDecoder and
	// TestEncoder assert nothing about pixel content beyond the golden records, so
	// this is how a change to a decoded image gets judged by eye.
	flagSave = flag.Bool("save", false, "write decoded images to testout/ as PNG")
	// flagUpdate rewrites testdata/golden.txt from the current decoder output instead
	// of comparing against it. Always inspect the resulting diff, and the PNGs from
	// -save, before committing it.
	flagUpdate = flag.Bool("update", false, "rewrite testdata/golden.txt from current output")
)
