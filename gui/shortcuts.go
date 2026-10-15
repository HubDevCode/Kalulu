package gui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
)

// installShortcuts binds the keyboard. Plain keys act only when no text
// field has the focus, so typing in a search box never skips a track.
func (a *App) installShortcuts() {
	c := a.win.Canvas()
	c.SetOnTypedKey(func(ev *fyne.KeyEvent) {
		// A menu, dialog or pop-up is open: the keys belong to it, and a
		// text field inside it may just have lost the focus.
		if c.Overlays().Top() != nil && ev.Name != fyne.KeyEscape {
			return
		}
		if c.Focused() != nil {
			return
		}
		shift := false
		if d, ok := fyne.CurrentApp().Driver().(desktop.Driver); ok {
			shift = d.CurrentKeyModifiers()&fyne.KeyModifierShift != 0
		}
		switch ev.Name {
		case fyne.KeySpace:
			a.togglePlay()
		case fyne.KeyRight:
			if shift {
				a.seekBy(30)
			} else {
				a.seekBy(5)
			}
		case fyne.KeyLeft:
			if shift {
				a.seekBy(-30)
			} else {
				a.seekBy(-5)
			}
		case fyne.KeyUp:
			a.adjustVolume(2)
		case fyne.KeyDown:
			a.adjustVolume(-2)
		case fyne.KeyN:
			a.next()
		case fyne.KeyP:
			a.prev()
		case fyne.KeyS:
			a.toggleShuffle()
		case fyne.KeyR:
			a.cycleRepeat()
		case fyne.KeyO:
			a.toggleMono()
		case fyne.KeyM:
			a.playerBar.toggleMute()
		case fyne.KeyV:
			a.nowPlaying.vis.Cycle()
		case fyne.KeyF:
			a.showFullVisualizer()
		case fyne.KeyF11:
			if a.current == "player" {
				a.setImmersive(!a.immersive)
			} else {
				a.win.SetFullScreen(!a.win.FullScreen())
			}
		case fyne.KeyEscape:
			a.setImmersive(false)
		case fyne.KeyF1:
			a.showShortcuts()
		case fyne.KeyDelete:
			if a.current == "queue" {
				a.queue.removeSelected()
			}
		case fyne.KeyPlus, fyne.KeyEqual:
			a.adjustVolume(2)
		case fyne.KeyMinus:
			a.adjustVolume(-2)
		case fyne.KeyLeftBracket:
			a.setSpeed(max(0.25, a.state.Speed-0.1))
		case fyne.KeyRightBracket:
			a.setSpeed(min(2, a.state.Speed+0.1))
		}
	})

	ctrl := func(key fyne.KeyName, fn func()) {
		c.AddShortcut(&desktop.CustomShortcut{KeyName: key, Modifier: fyne.KeyModifierShortcutDefault}, func(fyne.Shortcut) { fn() })
	}
	ctrl(fyne.KeyF, func() { a.show("search") })
	ctrl(fyne.KeyO, a.openFiles)
	ctrl(fyne.KeyL, a.openURL)
	ctrl(fyne.KeyE, func() { a.show("eq") })
	ctrl(fyne.KeyB, a.toggleRightPane)
	ctrl(fyne.KeyComma, func() { a.show("settings") })
	ctrl(fyne.KeyQ, a.quit)
	ctrl(fyne.Key1, func() { a.show("queue") })
	ctrl(fyne.Key2, func() { a.show("search") })
	ctrl(fyne.Key3, func() { a.show("player") })
	ctrl(fyne.Key4, func() { a.show("eq") })
	ctrl(fyne.Key5, func() { a.show("history") })
	c.AddShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyO, Modifier: fyne.KeyModifierShortcutDefault | fyne.KeyModifierShift},
		func(fyne.Shortcut) { a.openFolder() })
}
