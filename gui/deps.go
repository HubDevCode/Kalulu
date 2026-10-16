package gui

import (
	"os/exec"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const prefDepsIgnore = "deps.ignore_missing"

// requiredTools are the programs Kalulu starts for playback. The AppImage
// does not carry them: without yt-dlp no YouTube track plays, and without
// ffmpeg neither do Opus, AAC and the streamed pages.
var requiredTools = []string{"yt-dlp", "ffmpeg"}

func lookPathFound(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// missingTools lists the required tools that found does not find.
func missingTools(found func(string) bool) []string {
	var out []string
	for _, name := range requiredTools {
		if !found(name) {
			out = append(out, name)
		}
	}
	return out
}

// wingetIDs are the winget packages of the required tools.
var wingetIDs = map[string]string{"yt-dlp": "yt-dlp.yt-dlp", "ffmpeg": "Gyan.FFmpeg"}

// installCommand is the command that installs missing with the package
// manager that found finds, or "" when none is known. The yt-dlp of the
// Debian and Ubuntu archives is often too old for YouTube, so there it
// comes from pipx. winget takes one package per command, one per line.
func installCommand(missing []string, found func(string) bool) string {
	if len(missing) == 0 {
		return ""
	}
	pkgs := strings.Join(missing, " ")
	switch {
	case found("pacman"):
		return "sudo pacman -S --needed " + pkgs
	case found("apt-get"):
		var apt []string
		ytdlp := false
		for _, name := range missing {
			if name == "yt-dlp" {
				ytdlp = true
				continue
			}
			apt = append(apt, name)
		}
		if !ytdlp {
			return "sudo apt install " + strings.Join(apt, " ")
		}
		return "sudo apt install " + strings.Join(append(apt, "pipx"), " ") + " && pipx ensurepath && pipx install yt-dlp"
	case found("dnf"):
		return "sudo dnf install " + pkgs
	case found("zypper"):
		return "sudo zypper install " + pkgs
	case found("brew"):
		return "brew install " + pkgs
	case found("winget"):
		var cmds []string
		for _, name := range missing {
			cmds = append(cmds, "winget install -e --id "+wingetIDs[name])
		}
		return strings.Join(cmds, "\n")
	}
	return ""
}

// checkTools tells the user, once the window is up, which playback tools
// are missing and how to install them.
func (a *App) checkTools() {
	if a.prefs.Bool(prefDepsIgnore) {
		return
	}
	go func() {
		missing := missingTools(lookPathFound)
		if len(missing) == 0 {
			return
		}
		cmd := installCommand(missing, lookPathFound)
		runOnUI(func() { a.showMissingTools(missing, cmd) })
	}()
}

func (a *App) showMissingTools(missing []string, cmd string) {
	msg := widget.NewLabel(T("Kalulu needs these programs to play music from YouTube and most streams, and they are not installed:") +
		"\n\n    " + strings.Join(missing, ", ") +
		"\n\n" + T("Local files can still play. Install them, then restart Kalulu."))
	msg.Wrapping = fyne.TextWrapWord
	content := container.NewVBox(msg)
	if cmd != "" {
		entry := widget.NewEntry()
		entry.SetText(cmd)
		entry.Wrapping = fyne.TextWrapWord
		entry.MultiLine = true
		content.Add(captionLabel(T("Run this command in a terminal:")))
		content.Add(entry)
		content.Add(container.NewHBox(flatButton(T("Copy command"), theme.ContentCopyIcon(), func() {
			a.fa.Clipboard().SetContent(cmd)
			a.toast(T("Command copied"))
		})))
	} else {
		content.Add(captionLabel(T("Install them with the package manager of your distribution.")))
	}
	ignore := widget.NewCheck(T("Don't remind me again"), func(on bool) { a.prefs.SetBool(prefDepsIgnore, on) })
	content.Add(ignore)
	d := dialog.NewCustom(T("Missing programs"), T("Close"), content, a.win)
	d.Resize(fyne.NewSize(520, 0))
	d.Show()
}
