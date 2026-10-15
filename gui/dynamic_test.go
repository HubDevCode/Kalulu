package gui

import (
	"image/color"
	"testing"
)

func TestPaletteFromCoverKeepsContrast(t *testing.T) {
	covers := []color.NRGBA{
		{0, 0, 0, 255}, {255, 255, 255, 255}, {255, 230, 0, 255}, {10, 20, 90, 255},
		{200, 30, 30, 255}, {30, 200, 90, 255}, {128, 128, 128, 255},
	}
	for _, base := range []Palette{guiAmpDark(), guiAmpLight()} {
		for _, c := range covers {
			p := paletteFromCover(base, c)
			if r := contrast(p.FG, p.BG); r < 7 {
				t.Errorf("%s + %v: text contrast %.1f", base.Name, c, r)
			}
			if r := contrast(p.Accent, p.BG); r < 4.5 {
				t.Errorf("%s + %v: accent contrast %.1f", base.Name, c, r)
			}
			if r := contrast(p.FGDim, p.BG); r < 4.5 {
				t.Errorf("%s + %v: dim text contrast %.1f", base.Name, c, r)
			}
		}
	}
}
