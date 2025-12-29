package pcx

import "image/color"

var cga16ColorPalette = [16]color.Color{
	color.RGBA{R: 0x00, G: 0x00, B: 0x00, A: 0xff}, //  0 black
	color.RGBA{R: 0x00, G: 0x00, B: 0xaa, A: 0xff}, //  1 blue
	color.RGBA{R: 0x00, G: 0xaa, B: 0x00, A: 0xff}, //  2 green
	color.RGBA{R: 0x00, G: 0xaa, B: 0xaa, A: 0xff}, //  3 cyan
	color.RGBA{R: 0xaa, G: 0x00, B: 0x00, A: 0xff}, //  4 red
	color.RGBA{R: 0xaa, G: 0x00, B: 0xaa, A: 0xff}, //  5 magenta
	color.RGBA{R: 0xaa, G: 0x55, B: 0x00, A: 0xff}, //  6 brown
	color.RGBA{R: 0xaa, G: 0xaa, B: 0xaa, A: 0xff}, //  7 light gray
	color.RGBA{R: 0x55, G: 0x55, B: 0x55, A: 0xff}, //  8 gray
	color.RGBA{R: 0x55, G: 0x55, B: 0xff, A: 0xff}, //  9 light blue
	color.RGBA{R: 0x55, G: 0xff, B: 0x55, A: 0xff}, // 10 light green
	color.RGBA{R: 0x55, G: 0xff, B: 0xff, A: 0xff}, // 11 light cyan
	color.RGBA{R: 0xff, G: 0x55, B: 0x55, A: 0xff}, // 12 light red
	color.RGBA{R: 0xff, G: 0x55, B: 0xff, A: 0xff}, // 13 light magenta
	color.RGBA{R: 0xff, G: 0xff, B: 0x55, A: 0xff}, // 14 yellow
	color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, // 15 white
}

var cga4ColorPalettes = [8][]color.Color{
	{cga16ColorPalette[2], cga16ColorPalette[4], cga16ColorPalette[6]},    // green, red, brown
	{cga16ColorPalette[10], cga16ColorPalette[12], cga16ColorPalette[14]}, // light green, light red, yellow
	{cga16ColorPalette[3], cga16ColorPalette[5], cga16ColorPalette[7]},    // cyan, magenta, light gray
	{cga16ColorPalette[11], cga16ColorPalette[13], cga16ColorPalette[15]}, // light cyan, light magenta, white
	{cga16ColorPalette[3], cga16ColorPalette[4], cga16ColorPalette[7]},    // cyan, red, light gray
	{cga16ColorPalette[11], cga16ColorPalette[12], cga16ColorPalette[15]}, // light cyan, light red, white
	{cga16ColorPalette[3], cga16ColorPalette[4], cga16ColorPalette[7]},    // cyan, red, light gray
	{cga16ColorPalette[11], cga16ColorPalette[12], cga16ColorPalette[15]}, // light cyan, light red, white
}

func isCGAPalette(pal color.Palette) bool {
	for _, c := range pal {
		if cgaPaletteIndex(c) < 0 {
			return false
		}
	}
	return true
}

func cgaPaletteIndex(c color.Color) int {
	for i, c2 := range cga16ColorPalette {
		if c == c2 {
			return i
		}
	}
	return -1
}
