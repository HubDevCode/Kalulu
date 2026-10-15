package gui

import (
	"image/color"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"

	cliamptheme "github.com/bjarneo/cliamp/theme"
)

// Palette is the resolved color set of the window. The cliamp themes give
// six foreground colors and an optional background; the surfaces are
// derived from the background.
type Palette struct {
	Name     string
	Light    bool
	BG       color.NRGBA // window background
	Surface  color.NRGBA // panels: sidebar, player bar
	Surface2 color.NRGBA // cards, hovered rows, inputs
	Border   color.NRGBA
	FG       color.NRGBA
	FGDim    color.NRGBA
	Accent   color.NRGBA
	Accent2  color.NRGBA // green of the theme, the second visualizer stop
	Warn     color.NRGBA
	Error    color.NRGBA
}

const (
	paletteDark  = "Kalulu Dark"
	paletteLight = "Kalulu Light"
)

func guiAmpDark() Palette {
	return Palette{
		Name:     paletteDark,
		BG:       hex("#0d0d12"),
		Surface:  hex("#14141c"),
		Surface2: hex("#1e1e2a"),
		Border:   hex("#262636"),
		FG:       hex("#f1f1f5"),
		FGDim:    hex("#9898ab"),
		Accent:   hex("#8b5cf6"),
		Accent2:  hex("#2dd4bf"),
		Warn:     hex("#fbbf24"),
		Error:    hex("#f87171"),
	}
}

func guiAmpLight() Palette {
	return Palette{
		Name:     paletteLight,
		Light:    true,
		BG:       hex("#f6f7fb"),
		Surface:  hex("#ffffff"),
		Surface2: hex("#eceef5"),
		Border:   hex("#dde0ea"),
		FG:       hex("#1b1e2b"),
		FGDim:    hex("#6b7186"),
		Accent:   hex("#6d4aff"),
		Accent2:  hex("#0891b2"),
		Warn:     hex("#d97706"),
		Error:    hex("#dc2626"),
	}
}

// PaletteNames lists the Kalulu palettes and then every cliamp theme.
func PaletteNames() []string {
	names := []string{paletteDark, paletteLight}
	for _, t := range cliamptheme.LoadAll() {
		names = append(names, t.Name)
	}
	return names
}

// PaletteByName resolves a palette name. Unknown names give Kalulu Dark.
func PaletteByName(name string) Palette {
	switch name {
	case paletteLight:
		return guiAmpLight()
	case "", paletteDark:
		return guiAmpDark()
	}
	for _, t := range cliamptheme.LoadAll() {
		if strings.EqualFold(t.Name, name) {
			return paletteFromTheme(t)
		}
	}
	return guiAmpDark()
}

func paletteFromTheme(t cliamptheme.Theme) Palette {
	base := guiAmpDark()
	if t.IsDefault() {
		return base
	}
	bg := base.BG
	if t.BG != "" {
		bg = hex(t.BG)
	}
	light := luminance(bg) > 0.5
	shift := func(c color.NRGBA, amount float64) color.NRGBA {
		if light {
			return mix(c, color.NRGBA{0, 0, 0, 255}, amount)
		}
		return mix(c, color.NRGBA{255, 255, 255, 255}, amount)
	}
	fg := hex(t.BrightFG)
	return Palette{
		Name:     t.Name,
		Light:    light,
		BG:       bg,
		Surface:  shift(bg, 0.035),
		Surface2: shift(bg, 0.08),
		Border:   shift(bg, 0.14),
		FG:       fg,
		FGDim:    mix(hex(t.FG), bg, 0.25),
		Accent:   hex(t.Accent),
		Accent2:  hex(t.Green),
		Warn:     hex(t.Yellow),
		Error:    hex(t.Red),
	}
}

// guiTheme adapts a Palette to fyne.Theme.
type guiTheme struct {
	p Palette
	// density scales the paddings (0.75 compact … 1.25 comfortable) and
	// textScale the text; zero means 1.
	density   float32
	textScale float32
}

var _ fyne.Theme = (*guiTheme)(nil)

func (t *guiTheme) Color(name fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	p := t.p
	switch name {
	case theme.ColorNameBackground:
		return p.BG
	case theme.ColorNameForeground:
		return p.FG
	case theme.ColorNamePlaceHolder, theme.ColorNameDisabled:
		return p.FGDim
	case theme.ColorNamePrimary, theme.ColorNameFocus, theme.ColorNameHyperlink:
		acc := p.Accent
		if d := dynAccent.Load(); d != nil {
			acc = *d
		}
		if name == theme.ColorNameFocus {
			return withAlpha(acc, 0x55)
		}
		return acc
	case theme.ColorNameForegroundOnPrimary:
		if luminance(p.Accent) > 0.6 {
			return color.NRGBA{0x10, 0x10, 0x14, 0xff}
		}
		return color.NRGBA{0xff, 0xff, 0xff, 0xff}
	case theme.ColorNameButton:
		return p.Surface2
	case theme.ColorNameDisabledButton:
		return withAlpha(p.Surface2, 0x80)
	case theme.ColorNameInputBackground:
		return p.Surface2
	case theme.ColorNameInputBorder:
		return p.Border
	case theme.ColorNameMenuBackground, theme.ColorNameOverlayBackground:
		return p.Surface
	case theme.ColorNameHeaderBackground:
		return p.Surface
	case theme.ColorNameHover:
		return withAlpha(p.FG, 0x12)
	case theme.ColorNamePressed:
		return withAlpha(p.FG, 0x22)
	case theme.ColorNameSelection:
		return withAlpha(p.Accent, 0x40)
	case theme.ColorNameSeparator:
		return p.Border
	case theme.ColorNameScrollBar:
		return withAlpha(p.FG, 0x40)
	case theme.ColorNameScrollBarBackground:
		return color.Transparent
	case theme.ColorNameShadow:
		if p.Light {
			return color.NRGBA{0, 0, 0, 0x30}
		}
		return color.NRGBA{0, 0, 0, 0x80}
	case theme.ColorNameSuccess:
		return p.Accent2
	case theme.ColorNameWarning:
		return p.Warn
	case theme.ColorNameError:
		return p.Error
	}
	variant := theme.VariantDark
	if p.Light {
		variant = theme.VariantLight
	}
	return theme.DefaultTheme().Color(name, variant)
}

func (t *guiTheme) Font(style fyne.TextStyle) fyne.Resource {
	return theme.DefaultTheme().Font(style)
}

func (t *guiTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return theme.DefaultTheme().Icon(name)
}

func (t *guiTheme) Size(name fyne.ThemeSizeName) float32 {
	d, k := t.density, t.textScale
	if d == 0 {
		d = 1
	}
	if k == 0 {
		k = 1
	}
	switch name {
	case theme.SizeNameText, theme.SizeNameHeadingText, theme.SizeNameSubHeadingText, theme.SizeNameCaptionText:
		return t.baseSize(name) * k
	case theme.SizeNamePadding, theme.SizeNameInnerPadding:
		return t.baseSize(name) * d
	}
	return t.baseSize(name)
}

func (t *guiTheme) baseSize(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNameText:
		return 13.5
	case theme.SizeNameHeadingText:
		return 22
	case theme.SizeNameSubHeadingText:
		return 16
	case theme.SizeNameCaptionText:
		return 11.5
	case theme.SizeNamePadding:
		return 5
	case theme.SizeNameInnerPadding:
		return 8
	case theme.SizeNameInputRadius:
		return 8
	case theme.SizeNameSelectionRadius:
		return 6
	case theme.SizeNameScrollBar:
		return 8
	case theme.SizeNameScrollBarSmall:
		return 4
	case theme.SizeNameSeparatorThickness:
		return 1
	}
	return theme.DefaultTheme().Size(name)
}

func hex(s string) color.NRGBA {
	s = strings.TrimPrefix(s, "#")
	if len(s) != 6 {
		return color.NRGBA{0x80, 0x80, 0x80, 0xff}
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return color.NRGBA{0x80, 0x80, 0x80, 0xff}
	}
	return color.NRGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 0xff}
}

func mix(a, b color.NRGBA, t float64) color.NRGBA {
	l := func(x, y uint8) uint8 { return uint8(float64(x) + (float64(y)-float64(x))*t + 0.5) }
	return color.NRGBA{l(a.R, b.R), l(a.G, b.G), l(a.B, b.B), l(a.A, b.A)}
}

func withAlpha(c color.NRGBA, a uint8) color.NRGBA {
	c.A = a
	return c
}

func luminance(c color.NRGBA) float64 {
	return (0.2126*float64(c.R) + 0.7152*float64(c.G) + 0.0722*float64(c.B)) / 255
}
