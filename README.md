<p align="center">
  <img src="packaging/kalulu.png" alt="Kalulu" width="128">
</p>

<h1 align="center">Kalulu</h1>

<p align="center">
  Un lettore musicale moderno, leggero e completo per Linux.<br>
  YouTube Music, radio, podcast, file locali e i tuoi servizi, con DJ Mixer, testi sincronizzati parola per parola e ascolto condiviso in rete locale.
</p>

<p align="center">
  <b>Versione 2.5.0</b> · ex GuiAmp · fork grafico di <a href="https://github.com/bjarneo/cliamp">cliamp</a> · licenza GPL-3.0
</p>

![La Home di Kalulu](docs/images/kalulu-home.png)

## Perché Kalulu

Kalulu unisce il motore di [cliamp](https://github.com/bjarneo/cliamp), un lettore da terminale veloce e collaudato, con una finestra moderna. Non serve nessun account: la Home propone musica di YouTube Music in base al paese, e ogni scheda parte con un clic. Se hai già dei servizi (Navidrome, Jellyfin, Plex, Spotify, Tidal, Qobuz…) li trovi nella Libreria.

![Il player con i testi sincronizzati](docs/images/kalulu-player.png)

## Funzioni

**Scoprire e ascoltare**
- Home modulare: mix fatti per te in base a cosa ascolti, YouTube Music, classifiche, generi e mood, radio e podcast del tuo paese. Scegli tu sezioni e ordine.
- Ricerca con suggerimenti mentre scrivi. Brani, artisti, album, video e playlist.
- Autoplay: quando la coda finisce, continua con brani simili.
- Radio di tutto il mondo ([Radio Browser](https://www.radio-browser.info)) e podcast delle classifiche Apple.
- File locali, cartelle, URL, playlist M3U/PLS, feed. Basta trascinarli sulla finestra.

**Audio**
- Avvio rapido: i brani YouTube partono dall'URL audio diretto, e i successivi in coda vengono preparati in anticipo.
- Qualità di streaming Opus: alta, bilanciata, risparmio dati o automatica in base alla connessione.
- Precaricamento senza pause tra i brani, salto del silenzio iniziale e finale, ripresa automatica dagli errori.
- Equalizzatore a 10 bande con preset, normalizzazione del volume, bilanciamento L/R, mono e velocità.

**DJ Mixer**
- Analizza tempo, battute, frasi, tonalità ed energia di ogni brano.
- Transizioni a tempo come un DJ: allineamento delle battute, adattamento del tempo, filtri, eco, dissolvenze.
- Preset Smooth, Beat Match, Long Mix, Quick Mix e Manuale, con durata, intensità e anteprima.

**Testi**
- Testi sincronizzati parola per parola, stile karaoke, con animazioni fluide e schermo intero.
- Nove fonti gratuite interrogate insieme, senza account: LyricsPlus, lrc.red e Paxsenix (testi Apple Music parola per parola), LRCLIB, BetterLyrics Unison, NetEase, QQ Music, Kugou e lyrics.ovh. Kalulu tiene il risultato migliore e lo salva.
- L'ingranaggio sotto i testi permette di cercare di nuovo, scegliere la fonte e regolare l'offset.

**Interfaccia**
- I colori della finestra seguono la copertina del brano, con contrasto sempre leggibile.
- Visualizer con la sfera NCS che pulsa a tempo, più barre, specchio, radiale e altri stili.
- Densità, dimensione del testo, barra laterale, animazioni ridotte: tutto personalizzabile.
- Icona nel tray: chiudendo la finestra la musica continua.
- Riapre la coda e il brano dove li avevi lasciati.
- Un'unica istanza: un secondo avvio porta in primo piano la finestra e le passa i file.
- Interfaccia in italiano e in inglese.

**Ascolto condiviso**
- Crea una stanza con un codice e ascoltate insieme sulla rete locale, sincronizzati al decimo di secondo.
- Ruoli: Owner, Editor, Su richiesta (propone brani da approvare) e Ascoltatore.
- Nessuna esposizione su Internet: la stanza risponde solo agli indirizzi della LAN.

**Leggero**
- Circa 300 MB di RAM durante la riproduzione, stabili anche dopo ore di ascolto.
- Copertine tenute alla dimensione in cui sono mostrate. Visualizer e animazioni si fermano quando la finestra è nascosta.

**Aggiornamenti**
- Controlla le release di questo repository. Scarica l'AppImage e la installa solo se coincide con il suo `SHA256SUMS`.

![Equalizzatore e audio](docs/images/kalulu-equalizer.png)

## Installazione

### Requisiti

| Cosa | Perché |
| --- | --- |
| `yt-dlp` | YouTube, YouTube Music, SoundCloud, Bandcamp |
| `ffmpeg` | AAC, Opus, ALAC, flussi di rete e analisi del DJ Mixer |
| `pipewire-alsa` o `pulseaudio-alsa` | uscita audio su PipeWire / PulseAudio |

### Compilare dai sorgenti

Servono Go (la versione indicata in `go.mod`), un compilatore C e le librerie di sviluppo di ALSA, OpenGL, X11 e Wayland.

```sh
# Arch, Manjaro, EndeavourOS
sudo pacman -S --needed base-devel go alsa-lib libglvnd libxcursor libxrandr libxinerama libxi libxxf86vm libxkbcommon wayland flac libvorbis libogg mpg123 yt-dlp ffmpeg

# Debian, Ubuntu, Mint, Pop!_OS
sudo apt install golang gcc pkg-config libasound2-dev libgl1-mesa-dev xorg-dev libxkbcommon-dev libwayland-dev libflac-dev libvorbis-dev libogg-dev libmpg123-dev yt-dlp ffmpeg

# Fedora
sudo dnf install golang gcc alsa-lib-devel mesa-libGL-devel libXcursor-devel libXrandr-devel libXinerama-devel libXi-devel libXxf86vm-devel libxkbcommon-devel wayland-devel flac-devel libvorbis-devel libogg-devel mpg123-devel yt-dlp ffmpeg
```

Poi:

```sh
git clone https://github.com/HubDevCode/Kalulu.git
cd Kalulu
make build      # crea ./kalulu
make install    # in ~/.local/bin, con voce nel menu applicazioni e icona
```

Le dipendenze di openSUSE, Void e Alpine sono in [docs/gui.md](docs/gui.md).

### AppImage e Flatpak

`make appimage` crea un `Kalulu-<versione>-<arch>.AppImage` portabile, con il suo file `SHA256SUMS`. Per il Flatpak c'è `packaging/flatpak/io.kalulu.Kalulu.yml`. I dettagli sono in [docs/gui.md](docs/gui.md).

## Uso

```sh
kalulu                          # apre la finestra
kalulu ~/Musica/album           # apre la finestra e riproduce una cartella
kalulu https://…                # un URL, una playlist, un feed
kalulu --background             # parte direttamente nel tray
kalulu --room "Festa"           # apre una stanza di ascolto condiviso
kalulu --join HOST:PORTA/CODICE # entra in una stanza
kalulu --tui                    # l'interfaccia da terminale di cliamp
kalulu -d                       # senza interfaccia, controllato dal socket
```

Premi `F1` nella finestra per vedere le scorciatoie da tastiera. Le scorciatoie con una sola lettera non si attivano mai mentre scrivi in una casella di testo.

## Configurazione

Kalulu usa la configurazione di cliamp in `~/.config/cliamp/config.toml`, quindi un'installazione di cliamp esistente funziona subito. Quasi tutto si regola da **Impostazioni**, divise in categorie: Generale, Personalizzazione, Home e servizi, Notifiche, Riproduzione, Transizioni, Audio, Testi, Visualizer, Rete, Download, Prestazioni, Plugin, Avanzate, Aggiornamenti.

Le preferenze si possono esportare e importare da Impostazioni → Avanzate. I servizi (Navidrome, Jellyfin, Plex, Emby, Spotify, Tidal, Qobuz, SoundCloud, Mixcloud…) si configurano in Impostazioni → Home e servizi. La guida di ogni servizio è nella cartella [docs](docs).

| Percorso | Contenuto |
| --- | --- |
| `~/.config/cliamp/config.toml` | configurazione del motore e dei servizi |
| `~/.config/cliamp/kalulu-session.json` | coda e brano dell'ultima sessione |
| `~/.config/cliamp/kalulu-crash.log` | rapporto di un eventuale crash |
| `~/.config/fyne/io.kalulu.Kalulu` | preferenze della finestra |
| `~/.cache/kalulu` | copertine, analisi DJ, testi |

## Documentazione

- [docs/gui.md](docs/gui.md): la finestra, Discover, DJ Mixer, testi, ascolto condiviso, memoria, installazione.
- [docs/remote-control.md](docs/remote-control.md): il controllo remoto via socket (API V2).
- [docs/plugins.md](docs/plugins.md): i plugin Lua di cliamp, che funzionano anche in Kalulu.
- [docs/configuration.md](docs/configuration.md): tutte le chiavi di `config.toml`.

## Sviluppo

```sh
make build     # compila ./kalulu
make test      # go test ./...
make preview   # disegna la finestra fuori schermo in build/preview/*.png
```

Il codice della finestra è nel pacchetto `gui/`. Parla con il motore di cliamp solo tramite le operazioni V2, in `gui_main.go`. Il resto del progetto:

| Pacchetto | Contenuto |
| --- | --- |
| `discover/` | YouTube Music senza account, podcast, radio |
| `djmix/` | analisi dei brani e pianificazione delle transizioni DJ |
| `player/` | motore audio, precaricamento, mix, silenzio, qualità, risoluzione rapida di YouTube |
| `lyrics/` | ricerca dei testi su più fonti, con cache |
| `share/` | ascolto condiviso in LAN |

## Riconoscimenti

Kalulu è un fork di [cliamp](https://github.com/bjarneo/cliamp) di Bjarne Øverli. Il motore, i servizi e i plugin vengono da lì: grazie! I testi arrivano dai servizi gratuiti citati sopra, e la scoperta musicale da YouTube Music, [Radio Browser](https://www.radio-browser.info) e dalle classifiche Apple Podcasts.

## Licenza

Kalulu è software libero: puoi ridistribuirlo e modificarlo secondo i termini della [GNU General Public License](LICENSE), versione 3 o (a tua scelta) qualsiasi versione successiva.

Kalulu contiene il codice di cliamp, © Bjarne Øverli, distribuito con licenza MIT. Come richiede quella licenza, il suo avviso originale è conservato in [LICENSES/MIT-cliamp.txt](LICENSES/MIT-cliamp.txt).
