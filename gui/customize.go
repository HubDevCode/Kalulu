package gui

import (
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const (
	prefHomeSections = "home.sections"  // ordered, enabled keys
	prefHiddenNav    = "sidebar.hidden" // hidden sidebar entries
	prefHiddenSrc    = "library.hidden" // hidden provider keys
	prefDensity      = "appearance.density"
	prefTextScale    = "appearance.text_scale"
	prefReduceMotion = "appearance.reduce_motion"
)

// homeSection is one block of Home that can be shown, hidden and moved.
type homeSection struct {
	key   string
	title string
}

func homeSections() []homeSection {
	return []homeSection{
		{"foryou", T("Made for you")},
		{"ytm", "YouTube Music"},
		{"charts", T("Charts")},
		{"moods", T("Moods & genres")},
		{"local", T("Your library")},
		{"servers", T("Your music servers")},
		{"radio", T("Radio")},
		{"podcasts", T("Podcasts")},
	}
}

var defaultHomeOrder = []string{"foryou", "ytm", "charts", "moods", "local", "servers", "radio", "podcasts"}

// homeOrder returns the enabled Home sections in order.
func (a *App) homeOrder() []string {
	raw := a.prefs.String(prefHomeSections)
	if raw == "" {
		return append([]string(nil), defaultHomeOrder...)
	}
	var out []string
	for _, k := range strings.Split(raw, ",") {
		if k = strings.TrimSpace(k); k != "" {
			out = append(out, k)
		}
	}
	return out
}

func (a *App) setHomeOrder(keys []string) {
	a.prefs.SetString(prefHomeSections, strings.Join(keys, ","))
	if h, ok := a.views["home"].(*homeView); ok {
		h.reload()
	}
}

func (a *App) prefSet(key string) map[string]bool {
	set := map[string]bool{}
	for _, k := range strings.Split(a.prefs.String(key), ",") {
		if k = strings.TrimSpace(k); k != "" {
			set[k] = true
		}
	}
	return set
}

func (a *App) togglePrefSet(key, item string, on bool) {
	set := a.prefSet(key)
	if on {
		set[item] = true
	} else {
		delete(set, item)
	}
	var out []string
	for k := range set {
		out = append(out, k)
	}
	a.prefs.SetString(key, strings.Join(out, ","))
}

// homeEditor lists the Home sections with a switch and arrows to reorder.
func (a *App) homeEditor() fyne.CanvasObject {
	box := container.NewVBox()
	var render func()
	render = func() {
		box.Objects = nil
		order := a.homeOrder()
		enabled := map[string]bool{}
		for _, k := range order {
			enabled[k] = true
		}
		// Enabled sections in their order, then the hidden ones.
		all := append([]string(nil), order...)
		for _, s := range homeSections() {
			if !enabled[s.key] {
				all = append(all, s.key)
			}
		}
		titles := map[string]string{}
		for _, s := range homeSections() {
			titles[s.key] = s.title
		}
		for i, key := range all {
			key, i := key, i
			if titles[key] == "" {
				continue
			}
			check := widget.NewCheck(titles[key], func(on bool) {
				cur := a.homeOrder()
				var next []string
				for _, k := range cur {
					if k != key {
						next = append(next, k)
					}
				}
				if on {
					next = append(next, key)
				}
				a.setHomeOrder(next)
				render()
			})
			check.Checked = enabled[key]
			move := func(delta int) {
				cur := a.homeOrder()
				for j, k := range cur {
					if k == key && j+delta >= 0 && j+delta < len(cur) {
						cur[j], cur[j+delta] = cur[j+delta], cur[j]
						a.setHomeOrder(cur)
						render()
						return
					}
				}
			}
			up := iconButton(theme.MoveUpIcon(), func() { move(-1) })
			down := iconButton(theme.MoveDownIcon(), func() { move(1) })
			if !enabled[key] {
				up.Disable()
				down.Disable()
			}
			_ = i
			box.Add(container.NewBorder(nil, nil, nil, container.NewHBox(up, down), check))
		}
		box.Refresh()
	}
	render()
	return box
}

// navEditor lets the user hide sidebar entries and library services.
func (a *App) navEditor() fyne.CanvasObject {
	hidden := a.prefSet(prefHiddenNav)
	box := container.NewGridWithColumns(2)
	entries := []struct{ key, label string }{
		{"home", T("Home")}, {"search", T("Search")}, {"radio", T("Radio")}, {"podcasts", T("Podcasts")},
		{"queue", T("Queue")}, {"dj", "DJ Mixer"}, {"eq", T("Equalizer")}, {"history", T("History")}, {"share", T("Together")},
	}
	for _, e := range entries {
		e := e
		c := widget.NewCheck(e.label, func(on bool) {
			a.togglePrefSet(prefHiddenNav, e.key, !on)
			a.applyNavVisibility()
		})
		c.Checked = !hidden[e.key]
		box.Add(c)
	}
	srcHidden := a.prefSet(prefHiddenSrc)
	src := container.NewGridWithColumns(2)
	for _, p := range a.Providers() {
		p := p
		if p.Key == "radio" || p.Key == "podcast" || p.Key == "podcasts" {
			continue
		}
		c := widget.NewCheck(p.Name, func(on bool) {
			a.togglePrefSet(prefHiddenSrc, p.Key, !on)
			a.applyNavVisibility()
		})
		c.Checked = !srcHidden[p.Key]
		src.Add(c)
	}
	return container.NewVBox(boldLabel(T("Sidebar")), box, boldLabel(T("Services in the Library")), src)
}

// applyNavVisibility shows or hides the sidebar entries.
func (a *App) applyNavVisibility() {
	hidden := a.prefSet(prefHiddenNav)
	srcHidden := a.prefSet(prefHiddenSrc)
	for key, n := range a.nav {
		show := !hidden[key]
		if strings.HasPrefix(key, "provider:") {
			show = !srcHidden[strings.TrimPrefix(key, "provider:")]
		}
		if show {
			n.Show()
		} else {
			n.Hide()
		}
	}
	if a.navBox != nil {
		a.navBox.Refresh()
	}
}

// resetAppearance restores the interface preferences after a confirmation;
// playback, transitions and services keep their values.
func (a *App) resetAppearance() {
	dialog.ShowConfirm(T("Reset the interface"), T("Restore the theme, colors, layout, Home sections and sidebar to their defaults? Playback, DJ and service settings are kept."), func(ok bool) {
		if !ok {
			return
		}
		for _, key := range []string{prefPalette, prefHomeSections, prefHiddenNav, prefHiddenSrc, prefDensity, prefTextScale,
			prefReduceMotion, prefDynamicColors, prefVisStyle, prefVisColors, prefVisIntensity, prefVisSensitivity,
			prefLyricsSize, prefLyricsAlign, prefLyricsSmooth, prefRightPane, prefBalance} {
			a.prefs.RemoveValue(key)
		}
		a.setPalette(paletteDark)
		a.toast(T("Interface restored"))
	}, a.win)
}

// reduceMotion reports whether the animations should be kept minimal.
func (a *App) reduceMotion() bool { return a.prefs.BoolWithFallback(prefReduceMotion, false) }

// makeTheme builds the Fyne theme of p with the size preferences.
func (a *App) makeTheme(p Palette) *guiTheme {
	return &guiTheme{
		p:         p,
		density:   float32(a.prefs.FloatWithFallback(prefDensity, 1)),
		textScale: float32(a.prefs.FloatWithFallback(prefTextScale, 1)),
	}
}

// customizeCard holds the layout and size preferences.
func (a *App) customizeCard() fyne.CanvasObject {
	densities := []string{T("Compact"), T("Normal"), T("Comfortable")}
	values := []float64{0.7, 1, 1.3}
	density := widget.NewSelect(densities, func(v string) {
		for i, x := range densities {
			if x == v && a.prefs.FloatWithFallback(prefDensity, 1) != values[i] {
				a.prefs.SetFloat(prefDensity, values[i])
				go runOnUI(func() { a.setPalette(a.prefs.StringWithFallback(prefPalette, paletteDark)) })
			}
		}
	})
	cur := a.prefs.FloatWithFallback(prefDensity, 1)
	density.Selected = densities[1]
	for i, v := range values {
		if v == cur {
			density.Selected = densities[i]
		}
	}
	text := widget.NewSlider(80, 140)
	text.Step = 5
	text.Value = a.prefs.FloatWithFallback(prefTextScale, 1) * 100
	tl := label(strconv.Itoa(int(text.Value)) + "%")
	text.OnChanged = func(v float64) { tl.SetText(strconv.Itoa(int(v)) + "%") }
	text.OnChangeEnded = func(v float64) {
		a.prefs.SetFloat(prefTextScale, v/100)
		go runOnUI(func() { a.setPalette(a.prefs.StringWithFallback(prefPalette, paletteDark)) })
	}
	motion := widget.NewCheck("", func(on bool) { a.prefs.SetBool(prefReduceMotion, on) })
	motion.Checked = a.reduceMotion()
	return settingsCard(T("Personalization"),
		setRow(T("Density"), T("Space between the elements"), density),
		setRow(T("Text size"), "", containerBorderRight(text, fixed(56, 0, tl))),
		setRow(T("Reduce motion"), T("Instant scrolling and color changes, fewer animations"), motion),
		widget.NewSeparator(),
		container.NewHBox(flatButton(T("Reset the interface"), theme.ViewRefreshIcon(), a.resetAppearance)),
	)
}

// homeServicesCard orders the Home sections and picks what the sidebar and
// the Library show.
func (a *App) homeServicesCard() fyne.CanvasObject {
	return settingsCard(T("Home and sidebar"),
		boldLabel(T("Home sections")),
		wrapLabel(T("Tick the sections to show and order them with the arrows. Sections without content, such as a service you have not set up, never appear.")),
		a.homeEditor(),
		widget.NewSeparator(),
		a.navEditor(),
	)
}
