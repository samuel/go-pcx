PCX Encoder/Decoder Package for Go
==================================

This package implements a PCX image decoder and encoder for Go.

Documentation: https://pkg.go.dev/github.com/samuel/go-pcx/pcx

Installation
------------

```sh
go get github.com/samuel/go-pcx/pcx
```

Usage
-----

Importing the package registers PCX with `image.Decode`, so a program that just needs
to read PCX files alongside other formats can import it for its side effects alone:

```go
import (
	"image"

	_ "github.com/samuel/go-pcx/pcx"
)

func load(r io.Reader) (image.Image, error) {
	img, _, err := image.Decode(r)
	return img, err
}
```

Decoding and encoding directly:

```go
package main

import (
	"log"
	"os"

	"github.com/samuel/go-pcx/pcx"
)

func main() {
	f, err := os.Open("in.pcx")
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	// The concrete type depends on the variant the file uses: *image.Paletted,
	// *image.Gray, *image.RGBA or *image.NRGBA. Use pcx.DecodeExtended instead to
	// also read the header metadata, such as DPI.
	img, err := pcx.Decode(f)
	if err != nil {
		log.Fatal(err)
	}

	out, err := os.Create("out.pcx")
	if err != nil {
		log.Fatal(err)
	}
	// pcx.Encode picks the PCX variant from the image's type and contents.
	if err := pcx.Encode(out, img); err != nil {
		log.Fatal(err)
	}
	if err := out.Close(); err != nil {
		log.Fatal(err)
	}
}
```

`pcx.Decode` reports a malformed file as a `pcx.FormatError` and a valid but
unimplemented variant as a `pcx.UnsupportedError`; a file that ends early is reported
as `io.ErrUnexpectedEOF`. `pcx.Encode` reports a caller mistake, such as bounds PCX
cannot represent, as a plain error.

Supported variants
------------------

| Bits per pixel | Planes | Decodes to | Palette |
|---|---|---|---|
| 8 | 1 | `*image.Paletted`, or `*image.Gray` when the file carries no palette and is marked grayscale | after the pixel data |
| 1, 2, 4 | 1 | `*image.Paletted` | header colormap (monochrome, CGA or EGA) |
| 1 | 2–4 | `*image.Paletted` | header colormap |
| 8 | 3 | `*image.RGBA` | — |
| 8 | 4 | `*image.NRGBA` (the fourth plane holds straight alpha) | — |

Uncompressed (non-RLE) PCX files are not supported.
