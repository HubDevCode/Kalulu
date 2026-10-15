package gui

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/bjarneo/cliamp/applog"
	"github.com/bjarneo/cliamp/djmix"
	"github.com/bjarneo/cliamp/ipc"
)

const (
	prefDJSettings = "dj.settings"
	prefPreload    = "preload.lead_sec"
)

// djSettings loads the transition settings from the preferences.
func (a *App) djSettings() djmix.Settings {
	s := djmix.DefaultSettings()
	if raw := a.prefs.String(prefDJSettings); raw != "" {
		_ = json.Unmarshal([]byte(raw), &s)
	}
	return s
}

func (a *App) setDJSettings(s djmix.Settings) {
	if data, err := json.Marshal(s); err == nil {
		a.prefs.SetString(prefDJSettings, string(data))
	}
	if a.opts.DJ != nil {
		a.opts.DJ.SetSettings(s)
	}
	a.updateLead()
}

// preloadLead is how early the next track is opened, in seconds.
func (a *App) preloadLead() float64 {
	return a.prefs.FloatWithFallback(prefPreload, 45)
}

// updateLead tells the player how early to preload: enough for the
// transition and at least the preload setting.
func (a *App) updateLead() {
	if a.opts.Mixer == nil {
		return
	}
	lead := time.Duration(a.preloadLead() * float64(time.Second))
	if a.opts.DJ != nil {
		lead = max(lead, a.opts.DJ.Lead())
	}
	a.opts.Mixer.SetTransitionLead(lead)
}

// analyzable reports whether a track can be analyzed: finite audio, not a
// radio stream.
func analyzable(t *ipc.TrackInfo) bool {
	if t == nil || t.Path == "" || t.Realtime || t.Feed {
		return false
	}
	if videoID(t.Path) != "" || isLocalPath(t.Path) {
		return true
	}
	return t.DurationSecs > 0 && strings.HasPrefix(t.Path, "http")
}

// djTick requests the analyses the next transition needs and reports a new
// transition.
func (a *App) djTick(s ipc.RuntimeSnapshot) {
	if a.opts.DJ == nil {
		return
	}
	if a.opts.DJ.Settings().Mode != djmix.ModeOff {
		if analyzable(s.Track) {
			a.opts.DJ.Request(s.Track.Path)
		}
		for _, i := range a.queue.upcoming(2) {
			t := a.queue.all[i]
			if analyzable(&t) {
				a.opts.DJ.Request(t.Path)
			}
		}
	}
	if a.opts.Mixer != nil {
		if ev, ok := a.opts.Mixer.LastTransition(); ok && ev.Started.After(a.lastMix) {
			a.lastMix = ev.Started
			label := djLabel(ev.Label)
			a.mixHistory = append([]mixRecord{{at: ev.Started, label: label, style: ev.Style}}, a.mixHistory...)
			if len(a.mixHistory) > 20 {
				a.mixHistory = a.mixHistory[:20]
			}
			if label != "" {
				a.toast("🎧 " + label)
				a.notify(prefNotifyMix, "Kalulu · DJ Mixer", label)
			}
			applog.Info("dj: transition %s (out at %v, %v long, in offset %v, rate %.4f)", ev.Label, ev.OutStart, ev.Length, ev.InOffset, ev.InRate)
		}
	}
}

type mixRecord struct {
	at    time.Time
	label string
	style string
}

var djWords = map[string]string{
	"Beatmatch blend": "Mix a tempo", "Filter sweep": "Sweep di filtro", "Echo out": "Uscita in eco",
	"Fade": "Dissolvenza", "Crossfade": "Dissolvenza incrociata", "Cut": "Taglio",
	" beats": " battiti", "energy up": "energia in salita", "energy down": "energia in discesa",
}

// djLabel translates a transition label.
func djLabel(label string) string {
	if uiLang != "it" {
		return label
	}
	for en, it := range djWords {
		label = strings.ReplaceAll(label, en, it)
	}
	return label
}

// djView is the DJ Mixer page.
type djView struct {
	app   *App
	root  fyne.CanvasObject
	modes []*modeCard
	deckA *deckCard
	deckB *deckCard
	next  *widget.Label
	when  *widget.Label
	live  *canvas.Rectangle
	hist  *fyne.Container
	opts  *fyne.Container
	histN int
}

func newDJView(a *App) *djView {
	v := &djView{app: a}
	st := a.djSettings()

	title := label("DJ Mixer")
	title.SizeName = theme.SizeNameHeadingText
	title.TextStyle = fyne.TextStyle{Bold: true}
	sub := dimLabel(T("Transitions like a real DJ: beatmatching, keys, filters and echoes"))
	sub.Wrapping = fyne.TextWrapWord
	p := a.theme.p
	glow := tGradient(func(p Palette) color.Color { return mix(p.Accent2, p.BG, 0.55) }, roleBG)
	header := container.NewStack(glow, padded(24, 24, 14, 24, container.NewVBox(title, sub)))

	type modeDef struct {
		mode  djmix.Mode
		title string
		desc  string
		icon  fyne.Resource
	}
	defs := []modeDef{
		{djmix.ModeOff, T("Gapless"), T("Tracks follow each other with no gap, as the album intends"), iconQueue},
		{djmix.ModeCrossfade, T("Crossfade"), T("Each track fades into the next over a few seconds"), iconShuffle},
		{djmix.ModeDJ, "DJ Mixer", T("Analyzes tempo, beats, key and energy, then mixes on the beat"), iconEqualizer},
	}
	var modeObjs []fyne.CanvasObject
	for _, d := range defs {
		d := d
		c := newModeCard(d.icon, d.title, d.desc, func() {
			s := a.djSettings()
			s.Mode = d.mode
			a.setDJSettings(s)
			v.refreshModes()
		})
		c.mode = d.mode
		v.modes = append(v.modes, c)
		modeObjs = append(modeObjs, c)
	}
	modeRow := container.NewGridWithColumns(3, modeObjs...)

	v.deckA = newDeckCard(a, T("Now playing"))
	v.deckB = newDeckCard(a, T("Up next"))
	decks := container.NewGridWithColumns(2, v.deckA.root, v.deckB.root)

	v.next = label("")
	v.next.Wrapping = fyne.TextWrapWord
	v.next.TextStyle = fyne.TextStyle{Bold: true}
	v.when = dimLabel("")
	v.live = canvas.NewRectangle(p.Accent2)
	v.live.CornerRadius = 5
	v.live.SetMinSize(fyne.NewSize(10, 10))
	nextCard := settingsCard(T("Next transition"),
		container.NewBorder(nil, nil, container.NewCenter(v.live), nil, v.next), v.when)

	v.opts = container.NewVBox()
	v.buildOptions(st)

	sortBtn := flatButton(T("Order the queue like a DJ"), iconShuffle, v.sortQueue)
	sortHelp := wrapLabel(T("Reorders the next tracks so tempos and keys flow into each other, as a DJ builds a set."))
	tools := settingsCard(T("Set builder"), sortHelp, container.NewHBox(sortBtn))

	v.hist = container.NewVBox(dimLabel(T("No transitions yet")))
	history := settingsCard(T("Transition history"), v.hist)

	body := container.NewVBox(header,
		padded(0, 24, 0, 24, container.NewVBox(
			sectionHeading(T("Mode")), modeRow,
			sectionHeading(T("Decks")), decks,
			nextCard,
			v.opts,
			tools,
			history,
			fixed(0, 16),
		)))
	v.root = container.NewVScroll(body)
	v.refreshModes()
	a.onState(v.update)
	return v
}

func (v *djView) Object() fyne.CanvasObject { return v.root }

func (v *djView) Shown() { v.update(v.app.state) }

func (v *djView) refreshModes() {
	mode := v.app.djSettings().Mode
	for _, c := range v.modes {
		c.setSelected(c.mode == mode)
	}
	v.buildOptions(v.app.djSettings())
}

// buildOptions shows the settings of the current mode.
func (v *djView) buildOptions(st djmix.Settings) {
	a := v.app
	save := func(change func(*djmix.Settings)) {
		s := a.djSettings()
		change(&s)
		a.setDJSettings(s)
	}
	check := func(text string, on bool, set func(*djmix.Settings, bool)) *widget.Check {
		c := widget.NewCheck(text, func(b bool) { save(func(s *djmix.Settings) { set(s, b) }) })
		c.Checked = on
		return c
	}
	slider := func(min, max, step, value float64, format func(float64) string, set func(*djmix.Settings, float64)) fyne.CanvasObject {
		s := widget.NewSlider(min, max)
		s.Step = step
		s.Value = value
		l := label(format(value))
		s.OnChanged = func(x float64) { l.SetText(format(x)) }
		s.OnChangeEnded = func(x float64) { save(func(st *djmix.Settings) { set(st, x) }) }
		return container.NewBorder(nil, nil, nil, fixed(70, 0, l), s)
	}
	preload := widget.NewSlider(15, 120)
	preload.Step = 5
	preload.Value = a.preloadLead()
	preloadL := label(fmt.Sprintf("%.0f s", preload.Value))
	preload.OnChanged = func(x float64) { preloadL.SetText(fmt.Sprintf("%.0f s", x)) }
	preload.OnChangeEnded = func(x float64) {
		a.prefs.SetFloat(prefPreload, x)
		a.updateLead()
	}
	preloadRow := row(T("Preload"), container.NewBorder(nil, nil, nil, fixed(70, 0, preloadL), preload))
	preloadHelp := wrapLabel(T("The next track is opened this long before the end, so it starts instantly, also when you skip to it."))

	var items []fyne.CanvasObject
	switch st.Mode {
	case djmix.ModeCrossfade:
		items = append(items,
			row(T("Length"), slider(1, 20, 0.5, st.CrossfadeSec, func(x float64) string { return fmt.Sprintf("%.1f s", x) },
				func(s *djmix.Settings, x float64) { s.CrossfadeSec = x })),
			check(T("Skip silence at the start and end of tracks"), st.SkipSilence, func(s *djmix.Settings, b bool) { s.SkipSilence = b }),
			check(T("Match the loudness of the next track"), st.LoudnessMatch, func(s *djmix.Settings, b bool) { s.LoudnessMatch = b }),
		)
	case djmix.ModeDJ:
		presetNames := map[string]string{"smooth": "Smooth", "beatmatch": "Beat Match", "long": "Long Mix", "quick": "Quick Mix", "manual": T("Manual")}
		var chips []fyne.CanvasObject
		for _, name := range djmix.Presets {
			name := name
			c := newChip(presetNames[name], func() {
				save(func(s *djmix.Settings) { s.ApplyPreset(name) })
				v.buildOptions(a.djSettings())
			})
			c.SetSelected(st.Preset == name)
			chips = append(chips, c)
		}
		manual := func(set func(*djmix.Settings, float64)) func(*djmix.Settings, float64) {
			return func(s *djmix.Settings, x float64) {
				set(s, x)
				s.Preset = "manual"
			}
		}
		items = append(items,
			setRow(T("Preset"), T("Smooth: soft blends · Beat Match: tight tempo sync · Long Mix: long layered mixes · Quick Mix: short cuts"), chipRow(chips...)),
			row(T("Mix length"), slider(4, 64, 1, st.MixSeconds, func(x float64) string { return fmt.Sprintf("%.0f s", x) },
				manual(func(s *djmix.Settings, x float64) { s.MixSeconds = x }))),
			wrapLabel(T("The length is rounded to whole bars at the tempo of the track, so every mix starts and ends on the beat.")),
			row(T("Intensity"), slider(10, 100, 5, st.Intensity*100, func(x float64) string { return fmt.Sprintf("%.0f%%", x) },
				manual(func(s *djmix.Settings, x float64) { s.Intensity = x / 100 }))),
			check(T("Beatmatch: sync the tempo of the next track"), st.BeatMatch, func(s *djmix.Settings, b bool) { s.BeatMatch = b; s.Preset = "manual" }),
			row(T("Pitch range"), slider(0, 12, 0.5, st.MaxTempoChange*100, func(x float64) string { return fmt.Sprintf("±%.1f%%", x) },
				manual(func(s *djmix.Settings, x float64) { s.MaxTempoChange = x / 100 }))),
			check(T("Harmonic mixing: longer blends for compatible keys"), st.HarmonicMixing, func(s *djmix.Settings, b bool) { s.HarmonicMixing = b; s.Preset = "manual" }),
			check(T("Effects: filter sweeps and echo outs when tempos differ"), st.Effects, func(s *djmix.Settings, b bool) { s.Effects = b; s.Preset = "manual" }),
			check(T("Mix over the outro of the outgoing track"), st.MixOutros, func(s *djmix.Settings, b bool) { s.MixOutros = b }),
			check(T("Match the loudness of the next track"), st.LoudnessMatch, func(s *djmix.Settings, b bool) { s.LoudnessMatch = b }),
			container.NewHBox(flatButton(T("Listen to the next transition"), theme.MediaPlayIcon(), v.previewTransition)),
			wrapLabel(T("Analysis needs ffmpeg (and yt-dlp for YouTube). Results are cached, so each track is analyzed once.")),
		)
	}
	items = append(items, preloadRow, preloadHelp)
	v.opts.Objects = []fyne.CanvasObject{settingsCard(T("Transition settings"), items...)}
	v.opts.Refresh()
}

// previewTransition jumps to a few seconds before the planned mix, so the
// next transition can be heard now.
func (v *djView) previewTransition() {
	a := v.app
	s := a.state
	up := a.queue.upcoming(1)
	if s.Track == nil || len(up) == 0 || a.opts.DJ == nil {
		a.toast(T("Queue another track to plan a transition"))
		return
	}
	next := a.queue.all[up[0]]
	an, _ := a.opts.DJ.Get(s.Track.Path)
	bn, _ := a.opts.DJ.Get(next.Path)
	t := djmix.Plan(an, bn, a.opts.DJ.Settings())
	start := t.OutStart.Seconds()
	if start <= 0 {
		start = s.Duration - t.Length.Seconds()
	}
	if start <= 0 || !s.Seekable {
		a.toast(T("Analyzing the tracks…"))
		return
	}
	a.seekTo(math.Max(0, start-6))
	a.toast(T("The transition starts in 6 seconds"))
}

func (v *djView) update(s ipc.RuntimeSnapshot) {
	a := v.app
	if a.current != "dj" || a.opts.DJ == nil {
		return
	}
	var cur *ipc.TrackInfo
	if s.Track != nil {
		cur = s.Track
	}
	var next *ipc.TrackInfo
	if up := a.queue.upcoming(1); len(up) > 0 {
		t := a.queue.all[up[0]]
		next = &t
	}
	aA := v.deckA.show(cur, s.Position)
	aB := v.deckB.show(next, 0)

	st := a.opts.DJ.Settings()
	mixing := a.opts.Mixer != nil && a.opts.Mixer.Mixing()
	v.live.Hidden = !mixing
	v.live.Refresh()
	switch {
	case st.Mode == djmix.ModeOff:
		v.next.SetText(T("Gapless: the next track starts right after this one"))
		v.when.SetText("")
	case mixing:
		if ev, ok := a.opts.Mixer.LastTransition(); ok {
			v.next.SetText(djLabel(ev.Label))
		}
		v.when.SetText(T("Mixing now"))
	case cur == nil || next == nil:
		v.next.SetText(T("Queue another track to plan a transition"))
		v.when.SetText("")
	default:
		t := djmix.Plan(aA, aB, st)
		v.next.SetText(djLabel(t.Label))
		start := t.OutStart.Seconds()
		if start <= 0 {
			start = s.Duration - t.Length.Seconds()
		}
		if s.Duration > 0 && start > s.Position {
			v.when.SetText(fmt.Sprintf(T("Starts in %s · lasts %s"), fmtDuration(start-s.Position), fmtDuration(t.Length.Seconds())))
		} else {
			v.when.SetText("")
		}
		v.deckA.setMarker(start)
		if aA == nil || aB == nil {
			v.when.SetText(T("Analyzing the tracks…"))
		}
	}
	if len(a.mixHistory) != v.histN {
		v.histN = len(a.mixHistory)
		v.hist.Objects = nil
		for _, r := range a.mixHistory {
			v.hist.Add(container.NewBorder(nil, nil, captionLabel(r.at.Format("15:04")), nil, trunc(label(r.label))))
		}
		v.hist.Refresh()
	}
}

// sortQueue reorders the upcoming tracks into a DJ set: each next track is
// the one closest in tempo, key and energy to the previous one.
func (v *djView) sortQueue() {
	a := v.app
	if a.opts.DJ == nil {
		return
	}
	start := a.state.Index + 1
	if start >= len(a.queue.all) {
		a.toast(T("Queue another track to plan a transition"))
		return
	}
	end := min(len(a.queue.all), start+25)
	paths := make([]string, 0, end-start)
	for _, t := range a.queue.all[start:end] {
		paths = append(paths, t.Path)
		if analyzable(&t) {
			a.opts.DJ.Request(t.Path)
		}
	}
	seed := ""
	if a.state.Track != nil {
		seed = a.state.Track.Path
		a.opts.DJ.Request(seed)
	}
	revision := a.state.PlaylistRevision
	a.toast(T("Analyzing the tracks…"))
	go func() {
		get := func(path string) *djmix.Analysis {
			an, _ := a.opts.DJ.Wait(path, 90*time.Second)
			return an
		}
		prev := get(seed)
		left := append([]string(nil), paths...)
		var order []string
		for len(left) > 0 {
			best, bestCost := 0, math.Inf(1)
			for i, p := range left {
				if c := djCost(prev, get(p)); c < bestCost {
					best, bestCost = i, c
				}
			}
			order = append(order, left[best])
			prev = get(left[best])
			left = append(left[:best], left[best+1:]...)
		}
		runOnUI(func() {
			if a.state.PlaylistRevision != revision {
				a.toast(T("The queue changed; try again"))
				return
			}
			go v.applyOrder(start, paths, order)
		})
	}()
}

// djCost is how rough the transition from a to b is.
func djCost(a, b *djmix.Analysis) float64 {
	if a == nil || b == nil || a.BPM <= 0 || b.BPM <= 0 {
		return 50
	}
	r := a.BPM / b.BPM
	for _, f := range []float64{2, 0.5} {
		if x := a.BPM / (b.BPM * f); math.Abs(x-1) < math.Abs(r-1) {
			r = x
		}
	}
	cost := math.Abs(r-1) * 100
	cost += float64(djmix.CamelotDistance(a, b)) * 3
	cost += math.Abs(a.MeanEnergy-b.MeanEnergy) * 10
	return cost
}

func (v *djView) applyOrder(start int, current, order []string) {
	a := v.app
	pos := append([]string(nil), current...)
	for target, path := range order {
		from := -1
		for i := target; i < len(pos); i++ {
			if pos[i] == path {
				from = i
				break
			}
		}
		if from < 0 || from == target {
			continue
		}
		if _, err := a.call("queue.move", P{"index": start + from, "to": start + target}); err != nil {
			a.toastError(err)
			return
		}
		moved := pos[from]
		pos = append(pos[:from], pos[from+1:]...)
		pos = append(pos[:target], append([]string{moved}, pos[target:]...)...)
	}
	a.refreshState()
	a.toast(T("Queue ordered for a smooth set"))
}

// modeCard is a selectable card of the mode picker.
type modeCard struct {
	widget.BaseWidget
	mode   djmix.Mode
	bg     *canvas.Rectangle
	border *canvas.Rectangle
	icon   *widget.Icon
	base   fyne.Resource
	title  *widget.Label
	desc   *widget.Label
	onTap  func()
}

func newModeCard(res fyne.Resource, title, desc string, onTap func()) *modeCard {
	c := &modeCard{onTap: onTap}
	c.bg = tRect(roleCard)
	c.bg.CornerRadius = 14
	c.border = canvas.NewRectangle(color.Transparent)
	c.border.CornerRadius = 14
	c.border.StrokeWidth = 2
	c.icon = widget.NewIcon(res)
	c.base = res
	c.title = boldLabel(title)
	c.desc = captionLabel(desc)
	c.desc.Wrapping = fyne.TextWrapWord
	c.ExtendBaseWidget(c)
	return c
}

func (c *modeCard) CreateRenderer() fyne.WidgetRenderer {
	head := container.NewHBox(fixed(28, 28, c.icon), c.title)
	return widget.NewSimpleRenderer(container.NewStack(c.bg, c.border, padded(12, 14, 12, 14, container.NewVBox(head, c.desc))))
}

func (c *modeCard) setSelected(v bool) {
	if v {
		c.border.StrokeColor = theme.Color(theme.ColorNamePrimary)
		c.icon.SetResource(theme.NewPrimaryThemedResource(c.base))
	} else {
		c.border.StrokeColor = color.Transparent
		c.icon.SetResource(c.base)
	}
	c.border.Refresh()
}

func (c *modeCard) Tapped(*fyne.PointEvent) {
	if c.onTap != nil {
		c.onTap()
	}
}

// deckCard shows one track with its analysis.
type deckCard struct {
	app    *App
	root   fyne.CanvasObject
	art    *artView
	title  *widget.Label
	artist *widget.Label
	bpm    *widget.Label
	key    *widget.Label
	keyBG  *canvas.Rectangle
	energy *widget.Label
	status *widget.Label
	wave   *canvas.Raster
	cur    *djmix.Analysis
	pos    float64
	marker float64
	path   string
}

func newDeckCard(a *App, caption string) *deckCard {
	d := &deckCard{app: a}
	d.art = newArtView(a, 72, 8)
	d.art.image.FillMode = canvas.ImageFillCover
	d.title = trunc(boldLabel("—"))
	d.artist = trunc(captionLabel(""))
	d.bpm = boldLabel("— BPM")
	d.key = boldLabel("—")
	d.keyBG = canvas.NewRectangle(theme.Color(theme.ColorNameButton))
	d.keyBG.CornerRadius = 8
	d.energy = captionLabel("")
	d.status = captionLabel("")
	d.wave = canvas.NewRaster(d.draw)
	info := container.NewBorder(nil, nil, d.art.Object(), nil, container.NewVBox(d.title, d.artist,
		container.NewHBox(d.bpm, container.NewStack(d.keyBG, d.key), d.energy)))
	d.root = settingsCard(caption, info, fixed(0, 56, d.wave), d.status)
	return d
}

// show displays t and returns its analysis when ready.
func (d *deckCard) show(t *ipc.TrackInfo, pos float64) *djmix.Analysis {
	dj := d.app.opts.DJ
	if t == nil {
		d.path = ""
		d.cur = nil
		d.title.SetText("—")
		d.artist.SetText("")
		d.bpm.SetText("— BPM")
		d.key.SetText("—")
		d.energy.SetText("")
		d.status.SetText("")
		d.art.Clear()
		d.wave.Refresh()
		return nil
	}
	if t.Path != d.path {
		d.path = t.Path
		title, sub := trackTitle(*t)
		d.title.SetText(title)
		d.artist.SetText(sub)
		d.art.Load(*t)
		d.marker = 0
	}
	an, ok := dj.Get(t.Path)
	switch {
	case ok:
		d.cur = an
		d.bpm.SetText(fmt.Sprintf("%.1f BPM", an.BPM))
		if c := an.Camelot(); c != "" {
			d.key.SetText(c + " · " + an.KeyName())
			d.keyBG.FillColor = camelotColor(an)
		} else {
			d.key.SetText("—")
		}
		d.keyBG.Refresh()
		d.energy.SetText(fmt.Sprintf(T("energy %d%%"), int(an.MeanEnergy*100)))
		d.status.SetText("")
	case !analyzable(t):
		d.cur = nil
		d.status.SetText(T("This source cannot be analyzed; it will use a crossfade"))
	case dj.Err(t.Path) != nil:
		d.cur = nil
		d.status.SetText(T("Analysis failed: ") + dj.Err(t.Path).Error())
	default:
		d.cur = nil
		d.status.SetText(T("Analyzing the tracks…"))
	}
	d.pos = pos
	d.wave.Refresh()
	return d.cur
}

func (d *deckCard) setMarker(sec float64) {
	if math.Abs(d.marker-sec) > 0.01 {
		d.marker = sec
		d.wave.Refresh()
	}
}

// camelotColor gives each wheel number its own hue, as DJ software does.
func camelotColor(a *djmix.Analysis) color.NRGBA {
	var n int
	fmt.Sscanf(a.Camelot(), "%d", &n)
	l := 0.36
	if !a.Minor {
		l = 0.44
	}
	return hsl(float64(n-1)/12, 0.6, l)
}

// draw renders the energy curve with intro and outro shading, the mix
// marker and the playhead.
func (d *deckCard) draw(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, max(1, w), max(1, h)))
	an := d.cur
	p := d.app.theme.p
	if an == nil || len(an.Energy) == 0 || w < 2 {
		for x := 0; x < w; x++ {
			setPx(img, x, h/2, withAlpha(p.FGDim, 0x50))
		}
		return img
	}
	dur := an.Duration
	for x := 0; x < w; x++ {
		sec := float64(x) / float64(w) * dur
		e := float64(an.Energy[min(len(an.Energy)-1, int(sec))])
		bar := int(e * float64(h-2))
		c := gradientAt(p, e)
		if sec < an.IntroEnd || sec > an.OutroStart {
			c = withAlpha(c, 0x80)
		}
		if sec < d.pos {
			c = mix(c, p.FG, 0.35)
		}
		for y := 0; y < bar; y++ {
			setPx(img, x, h-1-y, c)
		}
	}
	marker := func(sec float64, c color.NRGBA) {
		if sec <= 0 || sec > dur {
			return
		}
		x := int(sec / dur * float64(w))
		for y := 0; y < h; y++ {
			setPx(img, x, y, c)
			setPx(img, x+1, y, c)
		}
	}
	marker(d.marker, p.Warn)
	marker(d.pos, p.FG)
	return img
}
