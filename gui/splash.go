package gui

import (
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// splash covers the window while the engine starts. Each step shows what
// is really happening; it lifts as soon as the player can be used.
type splash struct {
	app     *App
	root    *fyne.Container
	steps   map[string]*splashStep
	order   []string
	started time.Time
	gone    bool
}

type splashStep struct {
	icon  *widget.Icon
	label *widget.Label
	done  bool
}

func newSplash(a *App) *splash {
	s := &splash{app: a, steps: map[string]*splashStep{}, started: time.Now()}
	logo := canvas.NewImageFromResource(LogoResource)
	logo.FillMode = canvas.ImageFillContain
	logo.SetMinSize(fyne.NewSize(112, 112))
	logo.CornerRadius = 24
	name := label("Kalulu")
	name.SizeName = theme.SizeNameHeadingText
	name.TextStyle = fyne.TextStyle{Bold: true}
	name.Alignment = fyne.TextAlignCenter
	bar := widget.NewProgressBarInfinite()
	list := container.NewVBox()
	for _, st := range []struct{ key, text string }{
		{"engine", T("Starting the audio engine")},
		{"sources", T("Loading your music sources")},
	} {
		step := &splashStep{icon: widget.NewIcon(theme.RadioButtonIcon()), label: dimLabel(st.text)}
		s.steps[st.key] = step
		s.order = append(s.order, st.key)
		list.Add(container.NewHBox(step.icon, step.label))
	}
	bg := tRect(roleBG)
	s.root = container.NewStack(bg, container.NewCenter(fixed(320, 0, container.NewVBox(
		container.NewCenter(logo), name, fixed(0, 8), bar, fixed(0, 8), list))))
	return s
}

func (s *splash) done() bool { return s.gone }

// step marks a startup step as finished and lifts the splash when all are.
func (s *splash) step(key string) {
	st, ok := s.steps[key]
	if !ok || st.done || s.gone {
		return
	}
	st.done = true
	st.icon.SetResource(theme.NewSuccessThemedResource(theme.ConfirmIcon()))
	st.label.Importance = widget.MediumImportance
	st.label.Refresh()
	for _, k := range s.order {
		if !s.steps[k].done {
			return
		}
	}
	s.gone = true
	wait := max(0, 250*time.Millisecond-time.Since(s.started))
	go func() {
		time.Sleep(wait)
		runOnUI(func() {
			s.root.Hide()
			s.app.win.Content().Refresh()
		})
	}()
}
