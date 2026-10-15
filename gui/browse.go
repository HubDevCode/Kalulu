package gui

import (
	"fmt"
	"math/rand/v2"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/bjarneo/cliamp/ipc"
)

// browseView browses one provider. It keeps a stack of pages, so opening a
// playlist or an artist pushes a page and Back returns to the list.
type browseView struct {
	app   *App
	p     ipc.ProviderInfo
	root  *fyne.Container
	stack []fyne.CanvasObject
	home  fyne.CanvasObject

	playlists *EntryList
	loaded    bool
	status    *widget.Label
	catalog   int
}

func newBrowseView(a *App, p ipc.ProviderInfo) *browseView {
	b := &browseView{app: a, p: p}
	b.root = container.NewStack()
	b.home = b.buildHome()
	b.root.Objects = []fyne.CanvasObject{b.home}
	return b
}

func (b *browseView) Object() fyne.CanvasObject { return b.root }

func (b *browseView) Shown() {
	if !b.loaded {
		b.loaded = true
		b.loadPlaylists()
	}
}

func (b *browseView) push(page fyne.CanvasObject) {
	b.stack = append(b.stack, page)
	b.root.Objects = []fyne.CanvasObject{page}
	b.root.Refresh()
}

func (b *browseView) pop() {
	if len(b.stack) == 0 {
		return
	}
	b.stack = b.stack[:len(b.stack)-1]
	top := b.home
	if n := len(b.stack); n > 0 {
		top = b.stack[n-1]
	}
	b.root.Objects = []fyne.CanvasObject{top}
	b.root.Refresh()
}

func (b *browseView) backButton() *widget.Button {
	back := widget.NewButtonWithIcon(T("Back"), theme.NavigateBackIcon(), b.pop)
	back.Importance = widget.LowImportance
	return back
}

func (b *browseView) buildHome() fyne.CanvasObject {
	title := heading(b.p.Name)
	b.status = dimLabel(T("Loading…"))
	var actions []fyne.CanvasObject
	if b.p.Searchable {
		search := widget.NewEntry()
		search.SetPlaceHolder("Search " + b.p.Name + "…")
		search.ActionItem = widget.NewIcon(theme.SearchIcon())
		search.OnSubmitted = func(q string) {
			if q = strings.TrimSpace(q); q != "" {
				b.openSearch(q)
			}
		}
		actions = append(actions, fixed(240, 0, search))
	}
	if b.p.Key == "local" {
		newPl := widget.NewButtonWithIcon("New playlist", theme.ContentAddIcon(), func() { b.app.newPlaylistWith(nil) })
		newPl.Importance = widget.LowImportance
		actions = append(actions, newPl)
	}
	refresh := iconButton(theme.ViewRefreshIcon(), b.loadPlaylists)
	actions = append(actions, refresh)
	header := pageHeader(title, b.status, actions...)

	b.playlists = NewEntryList(b.app)
	b.playlists.SetPlaceholder("Nothing to show yet")

	tabs := []*container.TabItem{container.NewTabItemWithIcon(T("Playlists"), iconQueue, b.playlists.Object())}
	loaders := map[*container.TabItem]func(){}
	if b.p.BrowseArtists {
		obj, load := b.buildArtists()
		tab := container.NewTabItemWithIcon(T("Artists"), iconArtist, obj)
		loaders[tab] = load
		tabs = append(tabs, tab)
	}
	if b.p.BrowseAlbums {
		obj, load := b.buildAlbums()
		tab := container.NewTabItemWithIcon(T("Albums"), iconAlbum, obj)
		loaders[tab] = load
		tabs = append(tabs, tab)
	}
	var body fyne.CanvasObject = b.playlists.Object()
	if len(tabs) > 1 {
		t := container.NewAppTabs(tabs...)
		t.OnSelected = func(item *container.TabItem) {
			if load := loaders[item]; load != nil {
				load()
			}
		}
		body = padded(0, 10, 0, 10, t)
	}
	return container.NewBorder(header, nil, nil, nil, body)
}

func (b *browseView) loadPlaylists() {
	b.status.SetText(T("Loading…"))
	go func() {
		r, err := b.app.call("provider.playlists", P{"provider": b.p.Key, "limit": 2000})
		runOnUI(func() {
			if err != nil {
				b.status.SetText("Could not load: " + err.Error())
				b.playlists.SetPlaceholder(err.Error())
				b.playlists.SetEntries(nil)
				return
			}
			b.setPlaylists(r.Response.Playlists, r.Response.Total)
		})
	}()
}

func (b *browseView) setPlaylists(lists []ipc.PlaylistInfo, total int) {
	var entries []entry
	section := ""
	for _, pl := range lists {
		pl := pl
		if pl.Section != "" && pl.Section != section {
			section = pl.Section
			entries = append(entries, entry{Header: true, Title: strings.ToUpper(section)})
		}
		var sub []string
		if pl.TrackCount > 0 {
			sub = append(sub, fmt.Sprintf(T("%d tracks"), pl.TrackCount))
		}
		if pl.DurationSecs > 0 {
			sub = append(sub, fmtDuration(float64(pl.DurationSecs)))
		}
		e := entry{
			Icon:   iconQueue,
			Title:  pl.Name,
			Sub:    strings.Join(sub, " · "),
			OnOpen: func() { b.openPlaylist(pl) },
			OnPlay: func() { b.playPlaylist(pl) },
			Menu:   func() *fyne.Menu { return b.playlistMenu(pl) },
		}
		if b.p.Key == "radio" || b.p.Key == "cliamp" {
			e.Icon = iconRadio
		}
		if pl.Favoritable {
			fav := pl.Favorite
			e.Star = &fav
			e.OnStar = func() {
				b.app.do("provider.favorite", P{"provider": b.p.Key, "playlist": pl.ID}, func(Result) { b.loadPlaylists() })
			}
		}
		entries = append(entries, e)
	}
	b.playlists.SetEntries(entries)
	count := len(lists)
	b.status.SetText(fmt.Sprintf("%d lists", count))
	if count == 0 {
		b.playlists.SetPlaceholder("No playlists here. Configure this source in Settings → Config file, or open a file.")
	}
	if b.p.Catalog {
		more := widget.NewButtonWithIcon(T("Load more"), theme.MoreHorizontalIcon(), func() { b.loadCatalog(count) })
		b.playlists.SetFooter(more)
	}
}

func (b *browseView) loadCatalog(offset int) {
	b.status.SetText("Loading more…")
	b.app.do("provider.catalog", P{"provider": b.p.Key, "offset": offset, "limit": 50}, func(r Result) {
		b.setPlaylists(r.Response.Playlists, r.Response.Total)
	})
}

func (b *browseView) playlistMenu(pl ipc.PlaylistInfo) *fyne.Menu {
	items := []*fyne.MenuItem{
		fyne.NewMenuItemWithIcon(T("Open"), theme.FolderOpenIcon(), func() { b.openPlaylist(pl) }),
		fyne.NewMenuItemWithIcon(T("Play"), theme.MediaPlayIcon(), func() { b.playPlaylist(pl) }),
		fyne.NewMenuItemWithIcon(T("Add all to queue"), theme.ContentAddIcon(), func() { b.addPlaylist(pl, false) }),
		fyne.NewMenuItemWithIcon(T("Shuffle into queue"), iconShuffle, func() { b.addPlaylist(pl, true) }),
	}
	if pl.Favoritable {
		items = append(items, fyne.NewMenuItemWithIcon(T("Toggle favorite"), iconHeart, func() {
			b.app.do("provider.favorite", P{"provider": b.p.Key, "playlist": pl.ID}, func(Result) { b.loadPlaylists() })
		}))
	}
	if b.p.Key == "local" {
		items = append(items,
			fyne.NewMenuItemSeparator(),
			fyne.NewMenuItemWithIcon(T("Rename…"), theme.DocumentCreateIcon(), func() { b.renamePlaylist(pl) }),
			fyne.NewMenuItemWithIcon(T("Delete…"), theme.DeleteIcon(), func() { b.deletePlaylist(pl) }),
		)
	}
	return fyne.NewMenu("", items...)
}

func (b *browseView) playPlaylist(pl ipc.PlaylistInfo) {
	b.app.toast("Loading " + pl.Name + "…")
	b.app.do("provider.load", P{"provider": b.p.Key, "playlist": pl.ID}, func(r Result) {
		b.app.toast(fmt.Sprintf("Playing %s (%d tracks)", pl.Name, r.Response.Total))
	})
}

// addPlaylist appends a playlist to the queue without replacing it.
func (b *browseView) addPlaylist(pl ipc.PlaylistInfo, shuffle bool) {
	go func() {
		tracks, err := b.fetchAll(pl.ID)
		if err != nil {
			b.app.toastError(err)
			return
		}
		if shuffle {
			rand.Shuffle(len(tracks), func(i, j int) { tracks[i], tracks[j] = tracks[j], tracks[i] })
		}
		runOnUI(func() { b.app.addTracks(tracks, false) })
	}()
}

func (b *browseView) fetchAll(id string) ([]ipc.TrackInfo, error) {
	var all []ipc.TrackInfo
	for offset := 0; offset < 10000; offset += 200 {
		r, err := b.app.call("provider.tracks", P{"provider": b.p.Key, "playlist": id, "offset": offset, "limit": 200})
		if err != nil {
			return all, err
		}
		all = append(all, r.Response.Tracks...)
		if len(r.Response.Tracks) == 0 || len(all) >= r.Response.Total {
			break
		}
	}
	return all, nil
}

func (b *browseView) renamePlaylist(pl ipc.PlaylistInfo) {
	entry := widget.NewEntry()
	entry.SetText(pl.Name)
	dialog.ShowForm("Rename playlist", "Rename", "Cancel", []*widget.FormItem{widget.NewFormItem("Name", entry)}, func(ok bool) {
		name := strings.TrimSpace(entry.Text)
		if ok && name != "" && name != pl.Name {
			b.app.do("playlist.rename", P{"provider": "local", "playlist": pl.ID, "new_name": name}, func(Result) {
				b.loadPlaylists()
				go b.app.reloadLocalPlaylists()
			})
		}
	}, b.app.win)
}

func (b *browseView) deletePlaylist(pl ipc.PlaylistInfo) {
	dialog.ShowConfirm("Delete playlist", "Delete the playlist \""+pl.Name+"\"? The audio files are not touched.", func(ok bool) {
		if ok {
			b.app.do("playlist.delete", P{"provider": "local", "playlist": pl.ID}, func(Result) {
				b.loadPlaylists()
				go b.app.reloadLocalPlaylists()
			})
		}
	}, b.app.win)
}

// trackPage is a page of tracks with Play all and paging.
type trackPage struct {
	b       *browseView
	list    *TrackList
	sub     *widget.Label
	more    *widget.Button
	fetch   func(offset int) ([]ipc.TrackInfo, int, error)
	offset  int
	total   int
	onPlay  func()
	removal func(i int) // removes row i from a saved playlist
}

func (b *browseView) newTrackPage(title string, fetch func(offset int) ([]ipc.TrackInfo, int, error), playAll func()) *trackPage {
	tp := &trackPage{b: b, fetch: fetch, onPlay: playAll}
	tp.list = NewTrackList(b.app)
	tp.list.SetPlaceholder(T("Loading…"))
	tp.list.OnPlay = func(i int) { b.app.playTrack(tp.list.Tracks[i]) }
	tp.list.Menu = func(i int) *fyne.Menu {
		t := tp.list.Tracks[i]
		items := b.app.trackMenu(t)
		if tp.removal != nil {
			items = append(items, fyne.NewMenuItemSeparator(),
				fyne.NewMenuItemWithIcon(T("Remove from playlist"), theme.ContentRemoveIcon(), func() { tp.removal(i) }))
		}
		return fyne.NewMenu("", items...)
	}
	tp.list.IsCurrent = func(i int) bool {
		s := b.app.state
		return s.Track != nil && tp.list.Tracks[i].Path == s.Track.Path
	}
	tp.sub = dimLabel("")
	play := widget.NewButtonWithIcon(T("Play"), theme.MediaPlayIcon(), func() {
		if tp.onPlay != nil {
			tp.onPlay()
		}
	})
	play.Importance = widget.HighImportance
	add := widget.NewButtonWithIcon(T("Add to queue"), theme.ContentAddIcon(), func() { b.app.addTracks(tp.list.Tracks, false) })
	add.Importance = widget.LowImportance
	shuffle := widget.NewButtonWithIcon(T("Shuffle"), iconShuffle, func() {
		tracks := append([]ipc.TrackInfo(nil), tp.list.Tracks...)
		rand.Shuffle(len(tracks), func(i, j int) { tracks[i], tracks[j] = tracks[j], tracks[i] })
		b.app.addTracks(tracks, true)
	})
	shuffle.Importance = widget.LowImportance
	var saveAs *widget.Button
	saveAs = iconButton(iconPlaylistAdd, func() {
		item := b.app.addToPlaylistItem(tp.list.Tracks)
		pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(saveAs)
		widget.ShowPopUpMenuAtPosition(item.ChildMenu, b.app.win.Canvas(), pos.Add(fyne.NewPos(0, saveAs.Size().Height)))
	})
	tp.more = widget.NewButtonWithIcon(T("Load more"), theme.MoreHorizontalIcon(), tp.loadMore)
	tp.more.Hide()

	header := pageHeader(heading(title), tp.sub, b.backButton(), play, shuffle, add, saveAs)
	page := container.NewBorder(header, container.NewCenter(padded(4, 0, 8, 0, tp.more)), nil, nil, tp.list.Object())
	b.push(page)
	tp.loadMore()
	return tp
}

func (tp *trackPage) loadMore() {
	tp.more.Disable()
	go func() {
		tracks, total, err := tp.fetch(tp.offset)
		runOnUI(func() {
			tp.more.Enable()
			if err != nil {
				tp.list.SetPlaceholder(err.Error())
				tp.list.SetTracks(tp.list.Tracks)
				return
			}
			all := append(tp.list.Tracks, tracks...)
			tp.offset = len(all)
			tp.total = total
			tp.list.SetPlaceholder("No tracks")
			tp.list.SetTracks(all)
			dur := 0
			for _, t := range all {
				dur += t.DurationSecs
			}
			text := fmt.Sprintf(T("%d tracks"), len(all))
			if total > len(all) {
				text = fmt.Sprintf("%d of %d tracks", len(all), total)
			}
			if dur > 0 {
				text += " · " + fmtDuration(float64(dur))
			}
			tp.sub.SetText(text)
			tp.more.Hidden = len(tracks) == 0 || total <= len(all)
			tp.more.Refresh()
		})
	}()
}

func (tp *trackPage) reload() {
	tp.offset = 0
	tp.list.Tracks = nil
	tp.loadMore()
}

func (b *browseView) openPlaylist(pl ipc.PlaylistInfo) {
	var tp *trackPage
	tp = b.newTrackPage(pl.Name, func(offset int) ([]ipc.TrackInfo, int, error) {
		r, err := b.app.call("provider.tracks", P{"provider": b.p.Key, "playlist": pl.ID, "offset": offset, "limit": 200})
		if err != nil {
			return nil, 0, err
		}
		return r.Response.Tracks, r.Response.Total, nil
	}, func() { b.playPlaylist(pl) })
	if b.p.Key == "local" {
		tp.removal = func(i int) {
			b.app.do("playlist.remove", P{"provider": "local", "playlist": pl.ID, "index": i}, func(Result) { tp.reload() })
		}
	}
}

func (b *browseView) openSearch(query string) {
	var tp *trackPage
	tp = b.newTrackPage("“"+query+"” on "+b.p.Name, func(offset int) ([]ipc.TrackInfo, int, error) {
		r, err := b.app.call("provider.search", P{"provider": b.p.Key, "query": query, "offset": offset, "limit": 50})
		if err != nil {
			return nil, 0, err
		}
		return r.Response.Tracks, r.Response.Total, nil
	}, func() {
		if len(tp.list.Tracks) > 0 {
			b.app.addTracks(tp.list.Tracks, true)
		}
	})
}

func (b *browseView) buildArtists() (fyne.CanvasObject, func()) {
	list := NewEntryList(b.app)
	list.SetPlaceholder("Loading artists…")
	loaded := false
	filter := widget.NewEntry()
	filter.SetPlaceHolder("Filter artists…")
	var all []ipc.ArtistInfo
	render := func() {
		needle := strings.ToLower(strings.TrimSpace(filter.Text))
		var entries []entry
		for _, ar := range all {
			ar := ar
			if needle != "" && !strings.Contains(strings.ToLower(ar.Name), needle) {
				continue
			}
			sub := ""
			if ar.AlbumCount > 0 {
				sub = fmt.Sprintf("%d albums", ar.AlbumCount)
			}
			entries = append(entries, entry{Icon: iconArtist, Title: ar.Name, Sub: sub, OnOpen: func() { b.openArtist(ar) }})
		}
		list.SetEntries(entries)
	}
	filter.OnChanged = func(string) { render() }
	load := func() {
		if loaded {
			return
		}
		loaded = true
		go func() {
			var artists []ipc.ArtistInfo
			for offset := 0; offset < 20000; offset += 200 {
				r, err := b.app.call("provider.artists", P{"provider": b.p.Key, "offset": offset, "limit": 200})
				if err != nil {
					runOnUI(func() { list.SetPlaceholder(err.Error()) })
					return
				}
				artists = append(artists, r.Response.Artists...)
				if len(r.Response.Artists) == 0 || len(artists) >= r.Response.Total {
					break
				}
			}
			runOnUI(func() {
				all = artists
				list.SetPlaceholder("No artists")
				render()
			})
		}()
	}
	return container.NewBorder(padded(8, 8, 4, 8, filter), nil, nil, nil, list.Object()), load
}

func (b *browseView) openArtist(ar ipc.ArtistInfo) {
	list := NewEntryList(b.app)
	list.SetPlaceholder("Loading albums…")
	header := pageHeader(heading(ar.Name), dimLabel(T("Albums")), b.backButton())
	b.push(container.NewBorder(header, nil, nil, nil, list.Object()))
	go func() {
		r, err := b.app.call("provider.artist_albums", P{"provider": b.p.Key, "artist": ar.ID, "limit": 500})
		runOnUI(func() {
			if err != nil {
				list.SetPlaceholder(err.Error())
				return
			}
			list.SetPlaceholder("No albums")
			list.SetEntries(b.albumEntries(r.Response.Albums))
		})
	}()
}

func (b *browseView) albumEntries(albums []ipc.AlbumInfo) []entry {
	entries := make([]entry, 0, len(albums))
	for _, al := range albums {
		al := al
		var sub []string
		if al.Artist != "" {
			sub = append(sub, al.Artist)
		}
		if al.Year > 0 {
			sub = append(sub, fmt.Sprint(al.Year))
		}
		right := ""
		if al.TrackCount > 0 {
			right = fmt.Sprintf(T("%d tracks"), al.TrackCount)
		}
		entries = append(entries, entry{
			Icon: iconAlbum, Title: al.Name, Sub: strings.Join(sub, " · "), Right: right,
			OnOpen: func() { b.openAlbum(al) },
			OnPlay: func() { b.playAlbum(al) },
			Menu: func() *fyne.Menu {
				return fyne.NewMenu("",
					fyne.NewMenuItemWithIcon(T("Open"), theme.FolderOpenIcon(), func() { b.openAlbum(al) }),
					fyne.NewMenuItemWithIcon("Play album", theme.MediaPlayIcon(), func() { b.playAlbum(al) }),
				)
			},
		})
	}
	return entries
}

func (b *browseView) playAlbum(al ipc.AlbumInfo) {
	b.app.toast("Loading " + al.Name + "…")
	b.app.do("provider.load_album", P{"provider": b.p.Key, "album": al.ID}, nil)
}

func (b *browseView) openAlbum(al ipc.AlbumInfo) {
	title := al.Name
	if al.Artist != "" {
		title += " — " + al.Artist
	}
	b.newTrackPage(title, func(offset int) ([]ipc.TrackInfo, int, error) {
		r, err := b.app.call("provider.album_tracks", P{"provider": b.p.Key, "album": al.ID, "offset": offset, "limit": 200})
		if err != nil {
			return nil, 0, err
		}
		return r.Response.Tracks, r.Response.Total, nil
	}, func() { b.playAlbum(al) })
}

func (b *browseView) buildAlbums() (fyne.CanvasObject, func()) {
	list := NewEntryList(b.app)
	list.SetPlaceholder("Loading albums…")
	sorts := widget.NewSelect(nil, nil)
	sorts.PlaceHolder = "Sort"
	sortIDs := map[string]string{}
	var albums []ipc.AlbumInfo
	sortID := ""
	var load func(offset int)
	more := widget.NewButtonWithIcon(T("Load more"), theme.MoreHorizontalIcon(), func() { load(len(albums)) })
	more.Hide()
	load = func(offset int) {
		go func() {
			r, err := b.app.call("provider.albums", P{"provider": b.p.Key, "sort": sortID, "offset": offset, "limit": 100})
			runOnUI(func() {
				if err != nil {
					list.SetPlaceholder(err.Error())
					return
				}
				if offset == 0 {
					albums = nil
				}
				albums = append(albums, r.Response.Albums...)
				list.SetPlaceholder("No albums")
				list.SetEntries(b.albumEntries(albums))
				more.Hidden = len(r.Response.Albums) < 100
				more.Refresh()
				if len(sorts.Options) == 0 && len(r.Response.Sorts) > 0 {
					var labels []string
					for _, s := range r.Response.Sorts {
						sortIDs[s.Label] = s.ID
						labels = append(labels, s.Label)
					}
					sorts.Options = labels
					sorts.Refresh()
				}
			})
		}()
	}
	sorts.OnChanged = func(label string) {
		sortID = sortIDs[label]
		load(0)
	}
	list.SetFooter(more)
	loaded := false
	obj := container.NewBorder(padded(8, 8, 4, 8, container.NewBorder(nil, nil, captionLabel("Sort by"), nil, sorts)), nil, nil, nil, list.Object())
	return obj, func() {
		if !loaded {
			loaded = true
			load(0)
		}
	}
}
