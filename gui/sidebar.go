package gui

import (
	_ "embed"
	"fmt"
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

//go:embed assets/RuslanDisplay-Regular.ttf
var ruslanTTF []byte

// brandFont is Ruslan Display, the font of the Kalulu wordmark.
var brandFont = fyne.NewStaticResource("RuslanDisplay-Regular.ttf", ruslanTTF)

// wordmark is "KALULU" in the brand font.
func wordmark(size float32) *canvas.Text {
	t := canvas.NewText("KALULU", theme.Color(theme.ColorNameForeground))
	t.FontSource = brandFont
	t.TextSize = size
	return t
}

// Sidebar roles: the profile card takes a shade of the accent, the plan
// card at the foot a stronger one, so both follow the cover colors.
var (
	roleProfile role = func(p Palette) color.Color { return mix(p.Surface2, p.Accent, 0.16) }
	roleNavCard role = func(p Palette) color.Color { return mix(p.Surface, p.Surface2, 0.35) }
	rolePlan    role = func(p Palette) color.Color { return mix(p.Surface2, p.Accent, 0.42) }
)

// borderedCard is a rounded card with a thin border, as the Friends card
// of the mobile drawer.
func borderedCard(content fyne.CanvasObject) *fyne.Container {
	edge := tRect(roleBorder)
	edge.CornerRadius = 12
	fill := tRect(roleNavCard)
	fill.CornerRadius = 11
	return container.NewStack(edge, padded(1, 1, 1, 1, fill), padded(6, 6, 6, 6, content))
}

// navHeading is the small title of a sidebar card.
func navHeading(text string) *widget.Label {
	l := captionLabel(text)
	l.Importance = widget.MediumImportance
	return l
}

// rowDivider is the hairline between the menu rows of the footer.
func rowDivider() fyne.CanvasObject {
	r := tRect(roleBorder)
	r.SetMinSize(fyne.NewSize(1, 1))
	return r
}

func (a *App) buildSidebar() fyne.CanvasObject {
	// Profile card: the round logo, the wordmark and the menu.
	logo := canvas.NewImageFromResource(LogoResource)
	logo.FillMode = canvas.ImageFillContain
	logo.CornerRadius = 22
	ring := canvas.NewCircle(color.Transparent)
	ring.StrokeColor = theme.Color(theme.ColorNamePrimary)
	ring.StrokeWidth = 2
	avatar := fixed(44, 44, container.NewStack(padded(2, 2, 2, 2, logo), ring))
	logoTap := newTapArea(func() {
		if !a.navCompact() {
			return
		}
		pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(logo)
		widget.ShowPopUpMenuAtPosition(a.appMenu(), a.win.Canvas(), pos.Add(fyne.NewPos(0, 48)))
	})
	title := wordmark(21)
	greet := captionLabel(greeting())
	a.navSources = captionLabel(T("Loading sources…"))
	menuBtn := iconButton(theme.MenuIcon(), nil)
	menuBtn.OnTapped = func() {
		pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(menuBtn)
		widget.ShowPopUpMenuAtPosition(a.appMenu(), a.win.Canvas(), pos.Add(fyne.NewPos(0, menuBtn.Size().Height)))
	}
	names := container.NewVBox(padded(4, 0, 0, 4, title), container.New(&tightVBox{}, greet, a.navSources))
	profileBG := tRect(roleProfile)
	profileBG.TopLeftCornerRadius = 0
	profileBG.TopRightCornerRadius = 20
	profileBG.BottomRightCornerRadius = 20
	profileBG.BottomLeftCornerRadius = 0
	identity := container.NewBorder(nil, nil, container.NewVBox(container.NewStack(avatar, logoTap)), container.NewVBox(menuBtn), padded(0, 0, 0, 8, names))
	profile := container.NewStack(profileBG, padded(12, 6, 12, 10, identity))

	item := func(key string, res fyne.Resource, text string) *navItem {
		n := newNavItem(key, res, text, func() { a.show(key) })
		a.nav[key] = n
		return n
	}
	discoverTitle, playerTitle, libraryTitle := navHeading(T("Discover")), navHeading(T("Your music")), navHeading(T("Library"))
	discover := borderedCard(container.NewVBox(
		padded(0, 0, 0, 6, discoverTitle),
		item("home", theme.HomeIcon(), T("Home")),
		item("search", theme.SearchIcon(), T("Search")),
		item("radio", iconRadio, T("Radio")),
		item("podcasts", iconPodcast, T("Podcasts")),
	))
	player := borderedCard(container.NewVBox(
		padded(0, 0, 0, 6, playerTitle),
		item("queue", iconQueue, T("Queue")),
		item("dj", iconDJ, "DJ Mixer"),
		item("eq", iconEqualizer, T("Equalizer")),
		item("history", theme.HistoryIcon(), T("History")),
		item("share", iconGroup, T("Together")),
	))
	providersLoading := captionLabel(T("Loading sources…"))
	a.navBox = container.NewVBox(padded(0, 0, 0, 6, libraryTitle), padded(0, 0, 0, 10, providersLoading))
	library := borderedCard(a.navBox)
	a.navCards = []fyne.CanvasObject{discover, player, library}
	cards := container.NewVBox(discover, player, library)

	// Footer: menu rows with an arrow, then the plan card.
	whatsNew := newNavItem("", iconNewReleases, T("What's new"), func() {
		a.show("settings")
		go func() {
			time.Sleep(80 * time.Millisecond)
			runOnUI(func() {
				if s, ok := a.views["settings"].(*settingsView); ok {
					s.jump(T("Updates"))
				}
			})
		}()
	}).withChevron()
	settings := item("settings", theme.SettingsIcon(), T("Settings")).withChevron()
	rows := container.NewVBox(rowDivider(), whatsNew, rowDivider(), settings)
	a.navExtra = []*navItem{whatsNew}

	planCaption := captionLabel("Kalulu " + KaluluVersion)
	planCaption.Importance = widget.MediumImportance
	free := label(T("Free"))
	free.TextStyle = fyne.TextStyle{Bold: true}
	free.SizeName = theme.SizeNameSubHeadingText
	free.Importance = widget.SuccessImportance
	price := captionLabel(T("€0.0 / month · open source"))
	price.Importance = widget.MediumImportance
	quick := container.NewGridWithColumns(3,
		iconButton(theme.FileAudioIcon(), a.openFiles),
		iconButton(theme.FolderOpenIcon(), a.openFolder),
		iconButton(iconLink, a.openURL),
	)
	planShape := tRect(rolePlan)
	planShape.TopRightCornerRadius = 20
	planText := container.New(&tightVBox{}, planCaption, free, price)
	plan := container.NewStack(planShape, padded(8, 8, 8, 12, container.NewVBox(planText, quick)))
	bottom := container.NewVBox(padded(4, 8, 4, 8, rows), plan)

	scroll := container.NewVScroll(padded(10, 8, 10, 8, cards))
	bg := tRect(roleSurface)
	a.navCompactHide = []fyne.CanvasObject{title, greet, a.navSources, menuBtn, plan,
		discoverTitle, playerTitle, libraryTitle, providersLoading}
	return container.NewStack(bg, container.NewBorder(padded(0, 8, 0, 0, profile), bottom, nil, nil, scroll))
}

// setSidebarSources shows the number of sources in the profile card.
func (a *App) setSidebarSources(n int) {
	if a.navSources == nil {
		return
	}
	switch n {
	case 0:
		a.navSources.SetText(T("No sources"))
	case 1:
		a.navSources.SetText(T("1 source"))
	default:
		a.navSources.SetText(fmt.Sprintf(T("%d sources"), n))
	}
}
