package gui

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
)

// settingsFormat identifies a Kalulu settings export.
const (
	settingsFormat  = "kalulu-settings"
	settingsVersion = 1
)

// settingsFile is the exported settings: the window preferences and the
// engine's config.toml, so a copy restores a whole installation.
type settingsFile struct {
	Format      string          `json:"format"`
	Version     int             `json:"version"`
	App         string          `json:"app_version"`
	Exported    time.Time       `json:"exported"`
	Preferences json.RawMessage `json:"preferences"`
	ConfigTOML  string          `json:"config_toml,omitempty"`
}

// preferencesFile is where Fyne keeps the preferences of Kalulu.
func preferencesFile() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "fyne", AppID, "preferences.json")
}

// exportSettings writes the settings to path.
func (a *App) exportSettings(path string) error {
	prefs, err := os.ReadFile(preferencesFile())
	if err != nil {
		prefs = []byte("{}")
	}
	if !json.Valid(prefs) {
		return errors.New("the preferences file is damaged")
	}
	cfg, _ := os.ReadFile(a.opts.ConfigPath)
	data, err := json.MarshalIndent(settingsFile{
		Format: settingsFormat, Version: settingsVersion, App: a.opts.Version,
		Exported: time.Now().UTC(), Preferences: prefs, ConfigTOML: string(cfg),
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// readSettings validates an export before anything is applied.
func readSettings(path string) (settingsFile, error) {
	var f settingsFile
	data, err := os.ReadFile(path)
	if err != nil {
		return f, err
	}
	if len(data) > 8<<20 {
		return f, errors.New("the file is too large to be a settings export")
	}
	if err := json.Unmarshal(data, &f); err != nil {
		return f, fmt.Errorf("not a settings file: %w", err)
	}
	if f.Format != settingsFormat {
		return f, errors.New("not a Kalulu settings file")
	}
	if f.Version < 1 || f.Version > settingsVersion {
		return f, fmt.Errorf("settings version %d is not supported by this Kalulu", f.Version)
	}
	var prefs map[string]any
	if err := json.Unmarshal(f.Preferences, &prefs); err != nil {
		return f, errors.New("the preferences in the file are damaged")
	}
	for k := range prefs {
		if strings.ContainsAny(k, "\x00\n") {
			return f, errors.New("the preferences in the file are damaged")
		}
	}
	return f, nil
}

// importSettings replaces the preferences and config.toml with the file,
// keeping a backup of both, and restarts.
func (a *App) importSettings(path string) error {
	f, err := readSettings(path)
	if err != nil {
		return err
	}
	stamp := time.Now().Format("20060102-150405")
	backup := func(file string) {
		if data, err := os.ReadFile(file); err == nil {
			_ = os.WriteFile(file+".bak-"+stamp, data, 0o600)
		}
	}
	prefsPath := preferencesFile()
	backup(prefsPath)
	if err := os.MkdirAll(filepath.Dir(prefsPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(prefsPath, f.Preferences, 0o600); err != nil {
		return err
	}
	if strings.TrimSpace(f.ConfigTOML) != "" {
		backup(a.opts.ConfigPath)
		if err := os.WriteFile(a.opts.ConfigPath, []byte(f.ConfigTOML), 0o600); err != nil {
			return err
		}
	}
	return nil
}

// backupCard holds export and import.
func (a *App) backupCard() fyne.CanvasObject {
	export := flatButton(T("Export settings…"), theme.UploadIcon(), func() {
		d := dialog.NewFileSave(func(w fyne.URIWriteCloser, err error) {
			if err != nil || w == nil {
				return
			}
			path := w.URI().Path()
			w.Close()
			if err := a.exportSettings(path); err != nil {
				a.toastError(err)
				return
			}
			a.toast(T("Settings exported to ") + path)
		}, a.win)
		d.SetFileName("kalulu-settings.json")
		d.SetFilter(storage.NewExtensionFileFilter([]string{".json"}))
		d.Resize(fyne.NewSize(820, 560))
		d.Show()
	})
	imp := flatButton(T("Import settings…"), theme.DownloadIcon(), func() {
		d := dialog.NewFileOpen(func(r fyne.URIReadCloser, err error) {
			if err != nil || r == nil {
				return
			}
			path := r.URI().Path()
			r.Close()
			f, err := readSettings(path)
			if err != nil {
				a.toastError(err)
				return
			}
			msg := fmt.Sprintf(T("Replace the current settings with the ones exported on %s (Kalulu %s)? A backup of the current files is kept, then Kalulu restarts."),
				f.Exported.Local().Format("02/01/2006 15:04"), f.App)
			dialog.ShowConfirm(T("Import settings"), msg, func(ok bool) {
				if !ok {
					return
				}
				if err := a.importSettings(path); err != nil {
					a.toastError(err)
					return
				}
				a.restart()
			}, a.win)
		}, a.win)
		d.SetFilter(storage.NewExtensionFileFilter([]string{".json"}))
		d.Resize(fyne.NewSize(820, 560))
		d.Show()
	})
	return settingsCard(T("Import and export"),
		wrapLabel(T("One versioned file holds the Kalulu preferences and config.toml (services, EQ, plugins), to move them to another computer. Imported files are checked before they are applied.")),
		container.NewHBox(export, imp),
	)
}
