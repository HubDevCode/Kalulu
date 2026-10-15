package gui

import (
	"bytes"
	"context"
	"crypto/sha1"
	hexenc "encoding/hex"
	"image"
	"image/color"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/dhowden/tag"
	_ "github.com/fyne-io/image/ico"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"

	"github.com/bjarneo/cliamp/ipc"
)

// artMaxSide bounds the decoded cover size to keep memory flat.
const artMaxSide = 640

// artBudget bounds the decoded covers kept in memory, in bytes.
const artBudget = 24 << 20

// artSide picks the decoded size of a cover shown at size points on a
// screen of the given scale: a list thumbnail never holds a 640 px picture.
// The sizes are steps, so the cache shares one picture between views.
func artSide(size, scale float32) int {
	if size <= 0 {
		return artMaxSide
	}
	px := int(size * max(1, scale) * 1.1)
	for _, step := range []int{64, 128, 192, 256, 384, 512} {
		if px <= step {
			return step
		}
	}
	return artMaxSide
}

var artCache = struct {
	sync.Mutex
	images map[string]image.Image
	order  []string
	bytes  int
}{images: map[string]image.Image{}}

// dropArtCache forgets the decoded covers; they reload from the disk cache.
func dropArtCache() {
	artCache.Lock()
	defer artCache.Unlock()
	clear(artCache.images)
	artCache.order = nil
	artCache.bytes = 0
}

// imageBytes estimates the memory of a decoded image.
func imageBytes(img image.Image) int {
	if img == nil {
		return 0
	}
	b := img.Bounds()
	px := b.Dx() * b.Dy()
	switch img.(type) {
	case *image.YCbCr:
		return px * 3 / 2
	case *image.Gray:
		return px
	}
	return px * 4
}

func cachedArt(key string) (image.Image, bool) {
	artCache.Lock()
	defer artCache.Unlock()
	img, ok := artCache.images[key]
	return img, ok
}

func storeArt(key string, img image.Image) {
	artCache.Lock()
	defer artCache.Unlock()
	if old, ok := artCache.images[key]; ok {
		artCache.bytes -= imageBytes(old)
	} else {
		artCache.order = append(artCache.order, key)
	}
	artCache.images[key] = img
	artCache.bytes += imageBytes(img)
	for len(artCache.order) > 1 && (artCache.bytes > artBudget || len(artCache.order) > 256) {
		old := artCache.order[0]
		artCache.bytes -= imageBytes(artCache.images[old])
		delete(artCache.images, old)
		artCache.order = artCache.order[1:]
	}
}

func artKey(t ipc.TrackInfo) string {
	if t.AlbumArtURL != "" {
		return t.AlbumArtURL
	}
	if isLocalPath(t.Path) {
		return "file:" + t.Path
	}
	return ""
}

func isLocalPath(p string) bool {
	return p != "" && !strings.Contains(p, "://")
}

var artClient = &http.Client{Timeout: 12 * time.Second}

// fetchArt loads the cover of t. It returns nil when the track has none.
func fetchArt(t ipc.TrackInfo, side int) image.Image {
	base := artKey(t)
	if base == "" {
		return nil
	}
	key := base + "@" + strconv.Itoa(side)
	if img, ok := cachedArt(key); ok {
		return img
	}
	var img image.Image
	if u := t.AlbumArtURL; u != "" {
		u = sizedArtURL(u, side)
		switch {
		case strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://"):
			if img = diskArt(u); img == nil {
				if img = fetchRemoteArt(u); img != nil {
					img = shrink(img)
					saveDiskArt(u, img)
				}
			}
		case strings.HasPrefix(u, "file://"):
			img = decodeFile(strings.TrimPrefix(u, "file://"))
		case isLocalPath(u):
			img = decodeFile(u)
		}
	}
	if img == nil && isLocalPath(t.Path) {
		img = embeddedArt(t.Path)
		if img == nil {
			img = folderArt(filepath.Dir(t.Path))
		}
	}
	if img != nil {
		img = shrinkTo(img, side)
	}
	storeArt(key, img)
	return img
}

// artSlots bounds the parallel cover downloads, so a page of 60 cards
// does not open 60 connections at once.
var artSlots = make(chan struct{}, 6)

func fetchRemoteArt(u string) image.Image {
	artSlots <- struct{}{}
	defer func() { <-artSlots }()
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", "Kalulu")
	resp, err := artClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	return img
}

func decodeFile(path string) image.Image {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	img, _, err := image.Decode(io.LimitReader(f, 32<<20))
	if err != nil {
		return nil
	}
	return img
}

func embeddedArt(path string) image.Image {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	meta, err := tag.ReadFrom(f)
	if err != nil {
		return nil
	}
	pic := meta.Picture()
	if pic == nil || len(pic.Data) == 0 {
		return nil
	}
	img, _, err := image.Decode(bytes.NewReader(pic.Data))
	if err != nil {
		return nil
	}
	return img
}

var folderArtNames = []string{"cover", "folder", "front", "album", "albumart", "artwork"}

func folderArt(dir string) image.Image {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	for _, want := range folderArtNames {
		for _, e := range entries {
			name := strings.ToLower(e.Name())
			ext := filepath.Ext(name)
			if strings.TrimSuffix(name, ext) != want {
				continue
			}
			switch ext {
			case ".jpg", ".jpeg", ".png", ".webp", ".gif":
				if img := decodeFile(filepath.Join(dir, e.Name())); img != nil {
					return img
				}
			}
		}
	}
	return nil
}

func shrink(img image.Image) image.Image { return shrinkTo(img, artMaxSide) }

// shrinkTo scales img down so its longer side is at most side.
func shrinkTo(img image.Image, side int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= side && h <= side {
		return img
	}
	scale := float64(side) / float64(max(w, h))
	dst := image.NewRGBA(image.Rect(0, 0, int(float64(w)*scale), int(float64(h)*scale)))
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
	return dst
}

// artView shows a cover over a gradient placeholder.
type artView struct {
	app     *App
	root    fyne.CanvasObject
	image   *canvas.Image
	holder  *canvas.Rectangle
	note    *widget.Icon
	current string
	side    int // decoded size of the cover
	onTap   func()
	// onLoaded runs on the UI thread with each loaded cover, nil when the
	// item has none.
	onLoaded func(image.Image)
}

func newArtView(a *App, size, radius float32) *artView {
	v := &artView{app: a, side: artSide(size, a.win.Canvas().Scale())}
	v.holder = tRect(roleArtHolder)
	v.holder.CornerRadius = radius
	v.holder.SetMinSize(fyne.NewSize(size, size))
	v.image = canvas.NewImageFromImage(nil)
	v.image.Hidden = true // an empty cover-filled image crashes the software painter
	v.image.FillMode = canvas.ImageFillContain
	v.image.ScaleMode = canvas.ImageScaleSmooth
	v.image.CornerRadius = radius
	v.note = widget.NewIcon(iconAlbum)
	noteSize := size * 0.4
	if size <= 0 {
		noteSize = 96
	}
	holder := container.NewStack(v.holder, container.NewCenter(fixed(noteSize, noteSize, v.note)))
	tap := newTapArea(func() {
		if v.onTap != nil {
			v.onTap()
		}
	})
	if size > 0 {
		v.root = fixed(size, size, container.NewStack(holder, v.image, tap))
	} else {
		v.root = container.NewStack(holder, v.image, tap)
	}
	return v
}

func (v *artView) Object() fyne.CanvasObject { return v.root }

// Load shows the cover of t, loading it in the background.
func (v *artView) Load(t ipc.TrackInfo) {
	key := artKey(t)
	v.current = key
	if key == "" {
		v.Clear()
		return
	}
	if img, ok := cachedArt(key + "@" + strconv.Itoa(v.side)); ok {
		v.set(img)
		return
	}
	go func() {
		img := fetchArt(t, v.side)
		runOnUI(func() {
			if v.current == key {
				v.set(img)
			}
		})
	}()
}

// LoadURL shows the image at u, a remote or a local address.
func (v *artView) LoadURL(u string) {
	v.Load(ipc.TrackInfo{AlbumArtURL: u})
}

// SetPlaceholder changes the icon shown while there is no image.
func (v *artView) SetPlaceholder(res fyne.Resource) {
	v.note.SetResource(res)
}

func (v *artView) set(img image.Image) {
	if v.onLoaded != nil {
		v.onLoaded(img)
	}
	v.image.Image = img
	v.image.Hidden = img == nil
	v.note.Hidden = img != nil
	v.image.Refresh()
	v.note.Refresh()
}

func (v *artView) Clear() {
	v.current = ""
	v.set(nil)
}

// tapArea is an invisible tappable layer.
type tapArea struct {
	widget.BaseWidget
	onTap func()
}

func newTapArea(onTap func()) *tapArea {
	t := &tapArea{onTap: onTap}
	t.ExtendBaseWidget(t)
	return t
}

func (t *tapArea) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(canvas.NewRectangle(color.Transparent))
}

func (t *tapArea) Tapped(*fyne.PointEvent) {
	if t.onTap != nil {
		t.onTap()
	}
}

// artDir is the on-disk cache of cover thumbnails.
var artDir = func() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "kalulu", "covers")
}()

func artFile(u string) string {
	if artDir == "" {
		return ""
	}
	sum := sha1.Sum([]byte(u))
	return filepath.Join(artDir, hexenc.EncodeToString(sum[:])+".jpg")
}

// diskArt loads a cached thumbnail of u.
func diskArt(u string) image.Image {
	if f := artFile(u); f != "" {
		return decodeFile(f)
	}
	return nil
}

// saveDiskArt keeps a JPEG thumbnail of img for the next launches.
func saveDiskArt(u string, img image.Image) {
	f := artFile(u)
	if f == "" || os.MkdirAll(artDir, 0o755) != nil {
		return
	}
	out, err := os.CreateTemp(artDir, "tmp-*")
	if err != nil {
		return
	}
	if err := jpeg.Encode(out, img, &jpeg.Options{Quality: 86}); err != nil {
		out.Close()
		os.Remove(out.Name())
		return
	}
	out.Close()
	_ = os.Rename(out.Name(), f)
}

var googleSize = regexp.MustCompile(`=w\d+-h\d+`)

// sizedArtURL asks YouTube's image servers for a picture close to the size
// shown, so a thumbnail does not download and decode a large cover.
func sizedArtURL(u string, side int) string {
	if side >= artMaxSide {
		return u
	}
	switch {
	case strings.Contains(u, "googleusercontent.com/") || strings.Contains(u, "ggpht.com/"):
		return googleSize.ReplaceAllString(u, "=w"+strconv.Itoa(side)+"-h"+strconv.Itoa(side))
	case strings.Contains(u, "i.ytimg.com/"):
		return strings.Replace(u, "/maxresdefault.", "/hqdefault.", 1)
	}
	return u
}
