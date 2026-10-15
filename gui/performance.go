package gui

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
)

// cacheDirs are the caches Kalulu fills on disk, by label.
func cacheDirs() []struct{ label, dir string } {
	base, err := os.UserCacheDir()
	if err != nil {
		return nil
	}
	return []struct{ label, dir string }{
		{T("Covers"), filepath.Join(base, "kalulu", "covers")},
		{T("DJ track analysis"), filepath.Join(base, "kalulu", "analysis")},
	}
}

func dirSize(dir string) int64 {
	var n int64
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if info, err := d.Info(); err == nil {
				n += info.Size()
			}
		}
		return nil
	})
	return n
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

// performanceCard shows what Kalulu keeps on disk and how to lighten it.
func (a *App) performanceCard() fyne.CanvasObject {
	objs := []fyne.CanvasObject{
		wrapLabel(T("Kalulu pauses the visualizer, the lyrics animation and the polling when the window is hidden, and loads covers and Home sections only when they come on screen. To lighten it further, turn on Reduce motion in Personalization.")),
	}
	for _, c := range cacheDirs() {
		c := c
		size := label(humanSize(dirSize(c.dir)))
		clear := flatButton(T("Clear"), theme.DeleteIcon(), func() {
			_ = os.RemoveAll(c.dir)
			size.SetText(humanSize(0))
			a.toast(T("Cache cleared"))
		})
		objs = append(objs, setRow(c.label, T("Rebuilt automatically when needed"), container.NewHBox(size, clear)))
	}
	return settingsCard(T("Performance and cache"), objs...)
}
