package gui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/bjarneo/cliamp/discover"
	"github.com/bjarneo/cliamp/ipc"
)

func kindLabel(kind string) string {
	switch kind {
	case discover.KindAlbum:
		return T("Album")
	case discover.KindPlaylist:
		return T("Playlist")
	case discover.KindArtist:
		return T("Artist")
	case discover.KindPodcast:
		return T("Podcast")
	case discover.KindMood:
		return T("Moods & genres")
	}
	return ""
}

// newYTPage opens an album, playlist, artist or mood page of YouTube Music.
func newYTPage(a *App, it discover.Item) fyne.CanvasObject {
	root := container.NewStack(loadingBox(fmt.Sprintf(T("Loading %s…"), it.Title)))
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		page, err := a.ytm.Browse(ctx, it)
		runOnUI(func() {
			if err != nil {
				msg := dimLabel(err.Error())
				root.Objects = []fyne.CanvasObject{container.NewVBox(
					container.NewHBox(flatButton(T("Back"), theme.NavigateBackIcon(), a.back)), msg)}
				root.Refresh()
				return
			}
			switch it.Kind {
			case discover.KindAlbum, discover.KindPlaylist, discover.KindPodcast:
				root.Objects = []fyne.CanvasObject{ytListPage(a, it, page)}
			default:
				root.Objects = []fyne.CanvasObject{ytShelfPage(a, it, page)}
			}
			root.Refresh()
		})
	}()
	return root
}

// ytListPage shows the tracks of an album or a playlist.
func ytListPage(a *App, it discover.Item, page discover.Page) fyne.CanvasObject {
	tracks := ytTracks(page.Tracks)
	sub := page.Header.Subtitle
	if sub == "" {
		sub = it.Subtitle
	}
	total := 0
	for _, t := range tracks {
		total += t.DurationSecs
	}
	if !strings.Contains(sub, fmt.Sprint(len(tracks))) && len(tracks) > 0 {
		count := fmt.Sprintf(T("%d tracks"), len(tracks))
		if total > 0 {
			count += " · " + fmtDuration(float64(total))
		}
		if sub != "" {
			sub += " • "
		}
		sub += count
	}
	h := newHero(a, strings.ToUpper(kindLabel(it.Kind)), page.Header.Title, sub, firstNonEmpty(page.Header.Thumb, it.Thumb), false, a.back)
	h.buttons.Add(primaryButton(T("Play"), theme.MediaPlayIcon(), func() { a.playTracks(tracks, 0) }))
	h.buttons.Add(flatButton(T("Shuffle"), iconShuffle, func() { a.playTracks(shuffled(tracks), 0) }))
	h.buttons.Add(flatButton(T("Add to queue"), theme.ContentAddIcon(), func() { a.addTracks(tracks, false) }))
	var addBtn *widget.Button
	addBtn = flatButton("", iconPlaylistAdd, func() {
		item := a.addToPlaylistItem(tracks)
		pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(addBtn)
		widget.ShowPopUpMenuAtPosition(item.ChildMenu, a.win.Canvas(), pos.Add(fyne.NewPos(0, addBtn.Size().Height)))
	})
	h.buttons.Add(addBtn)

	list := NewTrackList(a)
	list.SetPlaceholder(T("No results for “%s”"))
	list.OnPlay = func(i int) { a.playTracks(tracks, i) }
	list.Menu = func(i int) *fyne.Menu { return fyne.NewMenu("", a.trackMenu(list.Tracks[i])...) }
	list.IsCurrent = func(i int) bool {
		s := a.state
		return s.Track != nil && list.Tracks[i].Path == s.Track.Path
	}
	list.SetTracks(tracks)
	return container.NewBorder(h.root, nil, nil, nil, list.Object())
}

// ytShelfPage shows an artist or a mood page as shelves.
func ytShelfPage(a *App, it discover.Item, page discover.Page) fyne.CanvasObject {
	circle := it.Kind == discover.KindArtist
	h := newHero(a, strings.ToUpper(kindLabel(it.Kind)), page.Header.Title, page.Header.Subtitle, firstNonEmpty(page.Header.Thumb, it.Thumb), circle, a.back)
	var songs []ipc.TrackInfo
	for _, s := range page.Shelves {
		if t := ytTracks(s.Items); len(t) > len(songs) && len(t)*2 >= len(s.Items) {
			songs = t
			break
		}
	}
	if len(songs) > 0 {
		h.buttons.Add(primaryButton(T("Play"), theme.MediaPlayIcon(), func() { a.playTracks(songs, 0) }))
		h.buttons.Add(flatButton(T("Shuffle"), iconShuffle, func() { a.playTracks(shuffled(songs), 0) }))
		h.buttons.Add(flatButton(T("Start radio"), iconRadio, func() { a.startRadio(songs[0]) }))
	}
	box := container.NewVBox(h.root)
	for _, s := range page.Shelves {
		box.Add(padded(0, 24, 0, 24, a.shelfObject(s)))
	}
	box.Add(fixed(0, 20))
	return container.NewVScroll(box)
}
