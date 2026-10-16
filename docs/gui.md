# Kalulu: the graphical interface

Kalulu (formerly GuiAmp) is a fork of cliamp with a graphical window. It runs
the cliamp engine unchanged -- the player, every provider, the playlists, the
Lua plugins, the media controls and the IPC socket -- and draws its own
interface on top of it with [Fyne](https://fyne.io).

```sh
kalulu                     # open the window
kalulu ~/Music/album       # open the window and play a folder
kalulu https://…/stream    # or a URL, a playlist, a feed
kalulu --background        # start in the tray, without the window
kalulu --room "Party"      # start and open a shared listening room
kalulu --join HOST:PORT/CODE
kalulu --tui               # the original cliamp terminal interface
kalulu -d                  # headless, controlled through the socket
```

Every cliamp flag and subcommand still works, for example
`kalulu --provider navidrome`, `kalulu next` or `kalulu remote capabilities`.

## How it works

The window does not reimplement the player. `runGUI` in `gui_main.go`
builds the same runtime as the TUI and runs the Model in headless mode. The
`gui` package talks to it through an in-process `gui.Engine`, which submits
the [V2 operations](remote-control.md) as jobs and waits for them. So a
feature that the socket offers is a feature of the window too.

Only one Kalulu runs at a time. A second `kalulu FILE` hands its arguments
to the running window through `$XDG_RUNTIME_DIR/kalulu.sock` and exits; the
window comes to the front.

## Discover: YouTube Music, radio and podcasts without an account

The `discover` package finds music without signing in to anything:

| Page | What it shows |
| --- | --- |
| Home | Modular: Made for you (from your listening history), YouTube Music, charts, moods and genres, your library, your music servers, radio and podcasts. Settings → Home and services shows, hides and orders the sections; a section without content never appears |
| Search | Suggestions while you type, then the top result, songs, artists, albums, videos and playlists from YouTube Music, and the matches of every other configured source |
| Radio | The [Radio Browser](https://www.radio-browser.info) directory of the country, genres, search and favorites |
| Podcasts | The Apple podcast charts of the country, by category, and search |

The country comes from the timezone of the system; nothing is sent to a
geolocation service. Change it in Settings → General. **Start radio** plays a
track followed by similar songs, and near the end of the queue Kalulu
appends similar songs by itself (Settings → Playback).

## Playback

| Feature | What it does |
| --- | --- |
| Fast start | YouTube tracks play from their direct audio URL, resolved by one yt-dlp process that stays loaded while you listen (it exits after 15 minutes without requests). The next tracks of the queue, and a song the pointer rests on, are resolved in advance, so a click usually starts the music in under a second and seeking is instant. Other yt-dlp sites keep the yt-dlp pipe |
| Session | Kalulu reopens with the queue and the track of the last run, paused where it stopped (`kalulu-session.json` in the config directory) |
| Preload | The next track opens before the end, so the change is gapless |
| Silence skip | The silence at the start and at the end of every track is skipped. Settings → Playback sets the threshold and the minimum length |
| Errors | A track that fails is retried once and then skipped (or playback stops, as chosen in Settings → Playback) |
| Streaming quality | YouTube plays Opus: High (~160 kbps), Balanced (~70 kbps), Data saver (~50 kbps) or Automatic, which follows the measured connection speed. The read-ahead buffer keeps slow connections playing (Settings → Network) |
| Downloads | Saved in the original format by default, without a needless lossy conversion; Opus, M4A, MP3 and FLAC are available (Settings → Downloads) |
| Normalization and balance | The Equalizer page levels the loudness of every track (with a limiter against clipping), moves the L/R balance and downmixes to mono |

## DJ Mixer

Settings → Transitions picks Gapless, Crossfade or DJ Mixer. The DJ Mixer
analyses each track (tempo, beat phase, downbeats, key, energy, intro and
outro) and plans every transition: it matches the tempo of the next track,
aligns the beats and the phrases, and uses fade, EQ blend, filter, echo or
cut transitions. The DJ Mixer page has the presets Smooth, Beat Match, Long
Mix, Quick Mix and Manual, the mix length, the intensity and a preview of
the next transition. Analyses are cached in `~/.cache/kalulu/analysis`.

## Lyrics and visualizer

Lyrics come from the file tags or the provider first, then from free
services that need no account, all asked at once: LyricsPlus, lrc.red and
Paxsenix (Apple Music lyrics timed word by word), LRCLIB, BetterLyrics
Unison, NetEase, QQ Music, Kugou and lyrics.ovh (plain text). Kalulu keeps
the best answer, word-timed over line-timed over plain, matched by title,
artist and duration, and remembers it in `~/.cache/kalulu/lyrics`.
Musixmatch is left out: without an account it answers with decoy lyrics.

Click the cover of the player bar for the player view with the lyrics; its
button opens it full screen. The gear under the lyrics looks them up again,
picks one source, and moves them earlier or later. The size, alignment and
animation are in Settings → Lyrics.

The visualizer styles are NCS (a sphere of particles that pulses on the
beat), Bars, Mirror, Radial and more, with sensitivity, intensity and
colors in Settings → Visualizer. It stops when the window is hidden.

## Listening together on the LAN

The Together page opens a room: Kalulu shows a room code and the address.
Others join with the code (found on the local network by UDP broadcast) or
with `IP:port` and the code. Every guest plays the same source on its own
device and stays in sync with the Owner: track, position and pause, with a
gradual speed correction of the drift. Local files of the Owner are served
over HTTP on the LAN, to authenticated guests only.

| Role | Can |
| --- | --- |
| Owner | Everything; changes the roles and removes people |
| Editor | Control playback and the queue |
| On request | Ask for tracks; the Owner approves or denies them |
| Listener | Listen |

The server answers only private (LAN) addresses and never opens a port on
the router. A dropped guest reconnects by itself with the same role.

## Window, colors and customization

The colors of the window follow the cover of the playing track, with a
guaranteed text contrast and a gradual change (Settings → Personalization).
The same section sets the theme, density, text size and reduced motion, and
resets the interface after a confirmation. Settings → Home and services
shows or hides each sidebar entry and library service.

Desktop notifications are all off by default: Settings → Notifications
turns on track changes, DJ transitions and errors, one at a time. The tray
icon keeps Kalulu playing when the window closes.

Settings → Advanced exports and imports every preference and
`config.toml` as one versioned, validated file.

## Memory and diagnostics

Kalulu keeps covers at the size they are shown and at most 24 MB of them
in memory, drops them when the window goes to the tray, and asks the Go
runtime to keep the heap close to what is in use. The DJ analyses stay on
disk, with only the last 48 in memory. The dynamic colors repaint only the
surfaces that follow the cover; the widget theme never changes on a track
change.

A crash writes its report to `kalulu-crash.log` in the config directory,
since a launch from the desktop has no terminal. `KALULU_PPROF=127.0.0.1:6061`
serves the Go profiler for a performance report.

## Updates

Kalulu checks the releases of
[HubDevCode/Kalulu](https://github.com/HubDevCode/Kalulu) every time it
starts. When a newer one is out, a panel shows its version, date and notes,
with the release page and the download of the AppImage or the Windows
installer. Settings → Updates checks by hand, and can turn the check at
start off. A download is installed only when it matches the `SHA256SUMS`
of the same release.

## Keyboard

Plain keys act when no text field has the focus. F1 shows them in the window.

| Key | Action |
| --- | --- |
| `Space` | Play / pause |
| `N` / `P` | Next / previous |
| `→` / `←` | Seek 5 s; with `Shift`, 30 s |
| `↑` / `↓`, `+` / `-` | Volume |
| `M` | Mute |
| `S` | Shuffle |
| `R` | Cycle repeat |
| `O` | Mono |
| `[` / `]` | Speed down / up |
| `V` | Next visualizer style |
| `F` / `F11` | Full screen visualizer / window |
| `Delete` | Remove the selected queue row |
| `Ctrl+F` | Search |
| `Ctrl+O` / `Ctrl+Shift+O` | Open files / folder |
| `Ctrl+L` | Open URL |
| `Ctrl+E` | Equalizer |
| `Ctrl+1` … `Ctrl+5` | Queue, Search, Player and lyrics, Equalizer, History |
| `Ctrl+B` | Now playing panel |
| `Ctrl+,` | Settings |
| `Ctrl+Q` | Quit |
| `F1` | Shortcut help |

## Configuration

Kalulu reads the cliamp configuration in `~/.config/cliamp/config.toml`, so
an existing cliamp setup works at once. The window keeps its own
preferences in the Fyne preferences of `io.kalulu.Kalulu`; the preferences
of GuiAmp are copied there on the first start.

## Install

### From source

Kalulu needs Go (the version in `go.mod`), a C compiler and the headers of
ALSA, OpenGL, X11 and Wayland:

| Distribution | Packages |
| --- | --- |
| Debian, Ubuntu, Mint, Pop!_OS | `gcc pkg-config libasound2-dev libgl1-mesa-dev xorg-dev libxkbcommon-dev libwayland-dev libflac-dev libvorbis-dev libogg-dev libmpg123-dev` |
| Fedora, RHEL, Nobara | `gcc alsa-lib-devel mesa-libGL-devel libXcursor-devel libXrandr-devel libXinerama-devel libXi-devel libXxf86vm-devel libxkbcommon-devel wayland-devel flac-devel libvorbis-devel libogg-devel mpg123-devel` |
| Arch, Manjaro, EndeavourOS | `base-devel alsa-lib libglvnd libxcursor libxrandr libxinerama libxi libxxf86vm libxkbcommon wayland flac libvorbis libogg mpg123` |
| openSUSE | `gcc alsa-devel Mesa-libGL-devel libXcursor-devel libXrandr-devel libXinerama-devel libXi-devel libXxf86vm-devel libxkbcommon-devel wayland-devel flac-devel libvorbis-devel libogg-devel mpg123-devel` |
| Void | `gcc pkg-config alsa-lib-devel libglvnd-devel libXcursor-devel libXrandr-devel libXinerama-devel libXi-devel libXxf86vm-devel libxkbcommon-devel wayland-devel libflac-devel libvorbis-devel libogg-devel mpg123-devel` |
| Alpine | `gcc musl-dev alsa-lib-dev mesa-dev libxcursor-dev libxrandr-dev libxinerama-dev libxi-dev libxxf86vm-dev libxkbcommon-dev wayland-dev flac-dev libvorbis-dev libogg-dev mpg123-dev` |

Then:

```sh
make build              # ./kalulu
make install            # ~/.local/bin, desktop entry and icon
sudo make install PREFIX=/usr/local
```

On PipeWire or PulseAudio systems the ALSA output needs `pipewire-alsa` or
`pulseaudio-alsa`, as for cliamp. `ffmpeg` adds AAC, ALAC, Opus and WMA, and
`yt-dlp` adds YouTube, SoundCloud, Bandcamp and the other yt-dlp sites.

`zenity`, `kdialog`, `qarma` or `yad`, when installed, give the open dialogs
the look of the desktop and multi-select; without them Kalulu uses its own
dialog.

### AppImage

`make appimage` builds a portable `Kalulu-<version>-<arch>.AppImage` and its `SHA256SUMS` with
`packaging/appimage/build.sh`. It bundles the codec libraries, whose names
change between distributions, and uses the host's OpenGL, X11, Wayland and
ALSA. Build it on the oldest distribution you want to support, since glibc
is not bundled. It needs [appimagetool](https://github.com/AppImage/appimagetool).

The AppImage does not carry `yt-dlp` and `ffmpeg`. When either is missing,
Kalulu says so at start, with the command that installs it on the
distribution (`pacman`, `apt`, `dnf` or `zypper`). On `apt` systems yt-dlp
comes from `pipx`, as the archive version is often too old for YouTube.
"Don't remind me again" hides the message.

### Windows

`make windows` builds `Kalulu-<version>-x64-setup.exe` and its `SHA256SUMS` with
`packaging/windows/build.sh`, in an MSYS2 MINGW64 shell with Go, the
mingw-w64 gcc, pkg-config, flac, libvorbis, libogg and mpg123, and
[Inno Setup 6](https://jrsoftware.org/isinfo.php). The script header shows
how to cross-compile it on Linux.

The installer carries `kalulu.exe`, its codec libraries, and the playback
tools in `tools\`: `yt-dlp.exe`, the yt-dlp zipapp `yt-dlp.pyz` with an
embeddable Python, and `ffmpeg.exe` and `ffprobe.exe` with their libraries.
Each one is checked against the checksums of its release, and the Python
against the hash pinned in the script. Kalulu puts `tools\` first in its
`PATH`. `yt-dlp.exe` is a frozen binary that unpacks itself on every call,
so YouTube tracks resolve through the zipapp instead, loaded once in the
bundled Python as on Linux: a track starts in a second or two. The bundled
yt-dlp updates itself once a day, as YouTube changes often. Kalulu installs for the current
user by default, in `%LOCALAPPDATA%\Programs\Kalulu`; the installer can also
install for all users. Settings and data stay in `%APPDATA%\cliamp`.

The in-app updater downloads the new installer, checks it against
`SHA256SUMS` and runs it silently; the installer starts Kalulu again.

### Flatpak

`packaging/flatpak/io.kalulu.Kalulu.yml` builds a Flatpak on the
freedesktop runtime. Run `go mod vendor` first, as Flatpak builds run
offline.

## Development

`make preview` renders the window offscreen with a fake engine to
`build/preview/*.png`, at several window sizes. It is the quickest way to
check a layout change without starting the player.
