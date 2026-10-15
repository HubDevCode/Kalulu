package gui

import (
	"os"
	"strings"
)

// uiLang is the language of the interface: "it" or "en".
var uiLang = detectLang()

// detectLang reads the locale of the session.
func detectLang() string {
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG", "LANGUAGE"} {
		v := strings.ToLower(os.Getenv(key))
		if v == "" || v == "c" || v == "posix" {
			continue
		}
		if strings.HasPrefix(v, "it") {
			return "it"
		}
		return "en"
	}
	return "en"
}

// langCode is the two-letter language of the interface, for the services.
func langCode() string { return uiLang }

// T translates an English interface string.
func T(s string) string {
	if uiLang == "it" {
		if t, ok := italian[s]; ok {
			return t
		}
	}
	return s
}

var italian = map[string]string{
	// Navigation
	"Home": "Home", "Search": "Cerca", "Radio": "Radio", "Podcasts": "Podcast",
	"Now Playing": "In riproduzione", "Queue": "Coda", "Lyrics": "Testi", "Equalizer": "Equalizzatore",
	"History": "Cronologia", "Library": "Libreria", "Discover": "Scopri", "Your music": "La tua musica",
	"Settings": "Impostazioni", "Back": "Indietro", "Loading sources…": "Caricamento sorgenti…",
	// Greetings and home
	"Good morning": "Buongiorno", "Good afternoon": "Buon pomeriggio", "Good evening": "Buonasera", "Good night": "Buonanotte",
	"Music for you in %s": "Musica per te in %s", "Change country": "Cambia paese",
	"Charts": "Classifiche", "Moods & genres": "Mood e generi", "Popular stations in %s": "Radio più ascoltate in %s",
	"Top podcasts in %s": "Podcast più ascoltati in %s", "Loading…": "Caricamento…", "Retry": "Riprova",
	"Could not reach YouTube Music: %s": "Impossibile contattare YouTube Music: %s",

	"Quick picks": "Scelte rapide", "Show all": "Mostra tutto",
	// Kinds
	"Song": "Brano", "Video": "Video", "Album": "Album", "Playlist": "Playlist", "Artist": "Artista",
	"Station": "Stazione", "Podcast": "Podcast", "Episode": "Episodio",
	"Songs": "Brani", "Videos": "Video", "Albums": "Album", "Artists": "Artisti", "Playlists": "Playlist",
	"Episodes": "Episodi", "Stations": "Stazioni", "Top result": "Risultato migliore",
	"Other sources": "Altre sorgenti",
	// Actions
	"Play": "Riproduci", "Shuffle": "Casuale", "Play now": "Riproduci ora", "Play next": "Riproduci dopo",
	"Add to queue": "Aggiungi alla coda", "Add to playlist": "Aggiungi a playlist", "New playlist…": "Nuova playlist…",
	"Start radio": "Avvia radio", "Track info": "Informazioni brano", "Go to artist": "Vai all'artista",
	"Go to album": "Vai all'album", "Open": "Apri", "Remove from queue": "Rimuovi dalla coda",
	"Move up": "Sposta su", "Move down": "Sposta giù", "Move to top": "Sposta in cima", "Move to bottom": "Sposta in fondo",
	"Add to favorites": "Aggiungi ai preferiti", "Remove from favorites": "Rimuovi dai preferiti",
	"Clear": "Svuota", "Save": "Salva", "Save as playlist": "Salva come playlist", "Download": "Scarica",
	"Open files…": "Apri file…", "Open folder…": "Apri cartella…", "Open URL / stream…": "Apri URL / stream…",
	"Keyboard shortcuts": "Scorciatoie da tastiera", "About Kalulu": "Informazioni su Kalulu", "Quit": "Esci",
	"Full screen": "Schermo intero", "Theme": "Tema", "Visualizer": "Visualizzatore",
	"Toggle now playing panel": "Mostra/nascondi pannello", "Download current track": "Scarica il brano corrente",
	"Save queue as playlist…": "Salva la coda come playlist…", "Remove from playlist": "Rimuovi dalla playlist",

	"Rename…": "Rinomina…", "Delete…": "Elimina…", "Add all to queue": "Aggiungi tutto alla coda",
	"Shuffle into queue": "Aggiungi in ordine casuale", "Toggle favorite": "Preferito sì/no",
	"Load more": "Carica altri", "Refresh": "Aggiorna", "Latest episode": "Ultimo episodio",
	// Search
	"What do you want to listen to?": "Cosa vuoi ascoltare?", "Browse all": "Sfoglia tutto",
	"Recent searches": "Ricerche recenti", "No results for “%s”": "Nessun risultato per “%s”",
	"Searching…": "Ricerca in corso…",
	// Queue
	"Up next": "In coda", "Filter queue…": "Filtra la coda…", "%d tracks": "%d brani", "1 track": "1 brano",
	"The queue is empty": "La coda è vuota",

	"Play something from Home, Search, Radio or Podcasts, or open your files": "Riproduci qualcosa da Home, Cerca, Radio o Podcast, oppure apri i tuoi file",

	"Up next (play-next queue)": "Prossimi (riproduci dopo)", "The queue ends here": "La coda finisce qui",
	"Clear queue": "Svuota la coda", "Remove all %d tracks from the queue?": "Rimuovere tutti i %d brani dalla coda?",
	"Similar songs added": "Aggiunti brani simili", "Radio started": "Radio avviata",
	// Player
	"Nothing playing": "Niente in riproduzione", "Pick something to play": "Scegli qualcosa da ascoltare",
	"LIVE": "LIVE", "Shuffle on": "Casuale attivo", "Shuffle off": "Casuale disattivo", "Repeat: %s": "Ripeti: %s",
	"Mono output": "Uscita mono", "Stereo output": "Uscita stereo", "Added %s": "Aggiunto %s",
	"Added %d tracks": "Aggiunti %d brani", "Playing next: %s": "Dopo: %s", "Opening %s…": "Apertura di %s…",
	"Loading %s…": "Caricamento di %s…", "Added to favorites ♥": "Aggiunto ai preferiti ♥",
	"Removed from favorites": "Rimosso dai preferiti", "Downloading the current track…": "Download del brano…",
	"Saved to %s": "Salvato in %s",
	// Radio
	"Trending": "Di tendenza", "Favorite stations": "Stazioni preferite", "Genres": "Generi",
	"Search stations…": "Cerca stazioni…", "Worldwide": "Tutto il mondo", "Add to favorite stations": "Aggiungi alle stazioni preferite",
	"Remove from favorite stations": "Rimuovi dalle stazioni preferite",
	// Podcasts
	"Search podcasts…": "Cerca podcast…", "Categories": "Categorie", "All": "Tutti",
	"News": "Notizie", "Comedy": "Comicità", "Society & Culture": "Società e cultura", "True Crime": "Cronaca nera",
	"Technology": "Tecnologia", "Science": "Scienza", "Business": "Economia", "Health & Fitness": "Salute e benessere",
	"Sports": "Sport", "Music": "Musica", "TV & Film": "TV e cinema", "Arts": "Arte", "Education": "Istruzione",
	"Fiction": "Narrativa", "Kids & Family": "Bambini e famiglia", "Leisure": "Tempo libero",

	"Religion & Spirituality": "Religione e spiritualità",
	// Settings
	"Appearance": "Aspetto", "Playback": "Riproduzione", "Country": "Paese", "Language": "Lingua",
	"Location": "Posizione",

	"Recommendations, charts, radio and podcasts follow this country.": "Consigli, classifiche, radio e podcast seguono questo paese.",
	"Autoplay similar songs when the queue ends":                       "Al termine della coda riproduci brani simili",
	"Show the now playing panel (on wide windows)":                     "Mostra il pannello in riproduzione (finestre larghe)",
	"Desktop notification on track change":                             "Notifica al cambio brano",
	"Keep playing in the system tray when the window closes":           "Continua nella tray quando chiudi la finestra",

	"Repeat": "Ripeti", "Sources & configuration": "Sorgenti e configurazione", "Lua plugins": "Plugin Lua",
	"About": "Informazioni", "Edit config.toml…": "Modifica config.toml…", "Open config folder": "Apri cartella config",

	"Add Kalulu to the application menu": "Aggiungi Kalulu al menu applicazioni",
	"Equalizer & audio":                  "Equalizzatore e audio", "Equalizer & audio…": "Equalizzatore e audio…",
	"Shape the sound of every source": "Modella il suono di ogni sorgente", "Preset": "Preset",

	"Audio": "Audio", "Speed": "Velocità", "Output device": "Dispositivo di uscita", "System default": "Predefinito di sistema",

	"Mono output (downmix both channels)": "Uscita mono (unisce i due canali)",
	"Recently played":                     "Ascoltati di recente", "Nothing played yet": "Ancora nulla in cronologia",
	"Clear history": "Cancella cronologia", "Forget every recently played track?": "Dimenticare tutti i brani ascoltati?",
	"Play something to see its lyrics": "Riproduci qualcosa per vederne il testo",
	"Looking for lyrics…":              "Ricerca del testo…", "No lyrics found": "Testo non trovato",
	"Synced lyrics · click a line to jump there": "Testo sincronizzato · clicca una riga per saltare lì",

	"Plain lyrics":            "Testo non sincronizzato",
	"Restart Kalulu to apply": "Riavvia Kalulu per applicare",

	"Title": "Titolo", "Time": "Durata",

	"Transitions like a real DJ: beatmatching, keys, filters and echoes": "Transizioni come un vero DJ: beatmatch, tonalità, filtri ed eco",

	"Gapless":   "Senza pause",
	"Crossfade": "Dissolvenza incrociata",

	"Tracks follow each other with no gap, as the album intends":    "I brani si susseguono senza pause, come nell'album",
	"Each track fades into the next over a few seconds":             "Ogni brano sfuma nel successivo in pochi secondi",
	"Analyzes tempo, beats, key and energy, then mixes on the beat": "Analizza tempo, battiti, tonalità ed energia e mixa a tempo",

	"Now playing":     "In riproduzione",
	"Next transition": "Prossima transizione",
	"Mode":            "Modalità",
	"Decks":           "Deck",

	"Order the queue like a DJ": "Ordina la coda come un DJ",

	"Reorders the next tracks so tempos and keys flow into each other, as a DJ builds a set.": "Riordina i prossimi brani perché tempi e tonalità scorrano l'uno nell'altro, come un DJ costruisce un set.",

	"Set builder":        "Costruisci il set",
	"Transition history": "Storico transizioni",
	"No transitions yet": "Ancora nessuna transizione",
	"Preload":            "Precarica",

	"The next track is opened this long before the end, so it starts instantly, also when you skip to it.": "Il brano successivo viene aperto con questo anticipo, così parte all'istante anche quando lo salti.",

	"Length": "Durata",

	"Skip silence at the start and end of tracks": "Salta il silenzio all'inizio e alla fine dei brani",
	"Match the loudness of the next track":        "Uniforma il volume del brano successivo",
	"Beatmatch: sync the tempo of the next track": "Beatmatch: sincronizza il tempo del brano successivo",

	"Pitch range":  "Escursione pitch",
	"Blend length": "Durata del mix",
	"beats":        "battiti",

	"Harmonic mixing: longer blends for compatible keys":      "Mix armonico: mix più lunghi tra tonalità compatibili",
	"Effects: filter sweeps and echo outs when tempos differ": "Effetti: sweep di filtro ed eco quando i tempi sono diversi",
	"Mix over the outro of the outgoing track":                "Mixa sull'outro del brano in uscita",

	"Fallback fade": "Dissolvenza di riserva",

	"Analysis needs ffmpeg (and yt-dlp for YouTube). Results are cached, so each track is analyzed once.": "L'analisi richiede ffmpeg (e yt-dlp per YouTube). I risultati restano in cache: ogni brano si analizza una volta sola.",

	"Transition settings": "Impostazioni transizioni",

	"Gapless: the next track starts right after this one": "Senza pause: il prossimo brano parte subito dopo questo",

	"Mixing now": "Mix in corso",

	"Queue another track to plan a transition": "Metti in coda un altro brano per pianificare una transizione",
	"Starts in %s · lasts %s":                  "Inizia tra %s · dura %s",
	"Analyzing the tracks…":                    "Analisi dei brani in corso…",
	"The queue changed; try again":             "La coda è cambiata, riprova",
	"Queue ordered for a smooth set":           "Coda ordinata per un set fluido",
	"energy %d%%":                              "energia %d%%",

	"This source cannot be analyzed; it will use a crossfade": "Questa sorgente non è analizzabile: userà una dissolvenza",

	"Analysis failed: ":          "Analisi non riuscita: ",
	"Your daily mix":             "Il tuo mix giornaliero",
	"Based on %s":                "In base a %s",
	"Made for you":               "Creato per te",
	"Because you listened to %s": "Perché hai ascoltato %s",
	"Your top artists":           "I tuoi artisti più ascoltati",
	"Restart now":                "Riavvia ora",

	"Some changes apply after a restart": "Alcune modifiche si applicano dopo il riavvio",

	"General":                             "Generale",
	"Transitions":                         "Transizioni",
	"Sources":                             "Sorgenti",
	"Plugins":                             "Plugin",
	"Advanced":                            "Avanzate",
	"Location and language":               "Posizione e lingua",
	"Now playing panel":                   "Pannello in riproduzione",
	"Shown on windows wider than 1180 px": "Visibile sulle finestre più larghe di 1180 px",

	"Desktop": "Desktop",

	"Keep playing in the tray":                            "Continua nella tray",
	"Closing the window leaves Kalulu in the system tray": "Chiudendo la finestra Kalulu resta nell'area di notifica",

	"Now": "Adesso",

	"Autoplay similar songs": "Riproduzione automatica di brani simili",

	"When the queue ends, keep going with songs like the last one": "Quando la coda finisce, continua con brani simili all'ultimo",

	"Between tracks": "Tra un brano e l'altro",

	"Crossfade blends tracks; DJ Mixer beatmatches them":   "La dissolvenza sfuma i brani; il DJ Mixer li mixa a tempo",
	"Open the next track this many seconds before the end": "Apri il brano successivo con questo anticipo sulla fine",

	"Open DJ Mixer": "Apri DJ Mixer",
	"Sound":         "Suono",
	"Services":      "Servizi",

	"Fill in a service and restart Kalulu; it then shows in the Library. Credentials stay in config.toml, shared with cliamp.": "Compila un servizio e riavvia Kalulu: comparirà nella Libreria. Le credenziali restano in config.toml, condiviso con cliamp.",

	"No plugin commands":   "Nessun comando plugin",
	"arguments (optional)": "argomenti (facoltativi)",

	"Run": "Esegui",

	"cliamp plugins work unchanged. Install one with: kalulu plugins install <source>": "I plugin di cliamp funzionano senza modifiche. Installane uno con: kalulu plugins install <sorgente>",

	"Command":            "Comando",
	"Arguments":          "Argomenti",
	"Open plugin folder": "Apri cartella plugin",

	"Show": "Mostra",

	"Configuration file": "File di configurazione",
	"Pick a command":     "Scegli un comando",
	"Running…":           "Esecuzione…",
	"Done.":              "Fatto.",
	"Cancel":             "Annulla",

	"Changes apply the next time Kalulu starts.": "Le modifiche si applicano al prossimo avvio di Kalulu.",
	"Kalulu is now in the application menu":      "Kalulu ora è nel menu applicazioni",

	"None": "Nessuno",

	"A graphical fork of cliamp by Bjarne Øverli. Same engine, same providers, same plugins.": "Un fork grafico di cliamp di Bjarne Øverli. Stesso motore, stessi servizi, stessi plugin.",

	"Playback at startup":                     "Riproduzione all'avvio",
	"Volume (dB)":                             "Volume (dB)",
	"Volume floor (dB)":                       "Volume minimo (dB)",
	"The quietest step of the volume control": "Il gradino più basso del controllo volume",

	"One": "Uno",
	"Off": "No",

	"Start playing at launch":           "Avvia la riproduzione all'apertura",
	"Long seek step (s)":                "Salto lungo (s)",
	"Equalizer preset":                  "Preset equalizzatore",
	"Source at startup (terminal mode)": "Sorgente all'avvio (modalità terminale)",

	"Audio output": "Uscita audio",

	"Changes here apply after a restart.":                                 "Queste modifiche si applicano dopo il riavvio.",
	"Empty uses the system default; the Equalizer page lists the devices": "Vuoto usa il predefinito di sistema; la pagina Equalizzatore elenca i dispositivi",

	"Sample rate": "Frequenza di campionamento",
	"Automatic":   "Automatica",
	"Buffer (ms)": "Buffer (ms)",

	"Raise it for unstable radio streams": "Aumentalo per le radio instabili",
	"Resampling quality":                  "Qualità del ricampionamento",

	"best":      "migliore",
	"Bit depth": "Profondità di bit",

	"32 is lossless for FFmpeg-decoded formats": "32 è senza perdite per i formati decodificati da FFmpeg",

	"Lyrics and files":           "Testi e file",
	"Lyrics offset (ms)":         "Anticipo testi (ms)",
	"Download folder":            "Cartella download",
	"Empty uses ~/Music/cliamp":  "Vuoto usa ~/Music/cliamp",
	"Start folder for files":     "Cartella iniziale per i file",
	"Terminal interface (--tui)": "Interfaccia terminale (--tui)",
	"These apply to kalulu --tui, the original cliamp interface.": "Valgono per kalulu --tui, l'interfaccia originale di cliamp.",

	"Visualizer rows":               "Righe del visualizzatore",
	"Visualizer follows the volume": "Il visualizzatore segue il volume",
	"Simplified view":               "Vista semplificata",
	"Hide the key hints":            "Nascondi i suggerimenti tasti",
	"Hide the settings pane":        "Nascondi il pannello impostazioni",
	"Show metadata":                 "Mostra metadati",
	"Expanded playlist":             "Playlist espansa",
	"Horizontal padding":            "Margine orizzontale",
	"Vertical padding":              "Margine verticale",
	"Low power mode":                "Risparmio energetico",
	"Log level":                     "Livello di log",
	"User":                          "Utente",
	"Password":                      "Password",
	"Enabled":                       "Attivo",
	"Quality":                       "Qualità",

	"Needs Spotify Premium. Sign in from the Spotify source after a restart.": "Richiede Spotify Premium. Accedi dalla sorgente Spotify dopo il riavvio.",
	"Optional: your own app from developer.spotify.com":                       "Facoltativo: la tua app da developer.spotify.com",
	"Needs a paid Tidal plan.":                                                "Richiede un abbonamento Tidal.",
	"Needs a Qobuz subscription.":                                             "Richiede un abbonamento Qobuz.",

	"Home, Search and the radio already work without an account; this adds your library.": "Home, Cerca e la radio funzionano già senza account; questo aggiunge la tua libreria.",

	"Browser session":                       "Sessione del browser",
	"Open whole playlists from list= links": "Apri le playlist intere dai link list=",

	"Items per list":                     "Elementi per lista",
	"Album order":                        "Ordine degli album",
	"Stream format":                      "Formato dello stream",
	"raw always plays the original file": "raw riproduce sempre il file originale",

	"Libraries": "Librerie",

	"Comma-separated; empty loads all":  "Separate da virgola; vuoto le carica tutte",
	"Show plugin tracks it cannot play": "Mostra i brani dei plugin non riproducibili",
	"Podcasts (terminal mode)":          "Podcast (modalità terminale)",
	"Chart country":                     "Paese della classifica",
	"Two letters, such as it or us":     "Due lettere, ad esempio it o us",

	"Pause":                "Pausa",
	"Next":                 "Successivo",
	"Previous":             "Precedente",
	"Stop":                 "Ferma",
	"Volume":               "Volume",
	"Volume up":            "Alza il volume",
	"Volume down":          "Abbassa il volume",
	"Mute":                 "Muto",
	"Show Kalulu":          "Mostra Kalulu",
	"Notify track changes": "Notifica il cambio brano",
	"A desktop notification for each new track":     "Una notifica desktop per ogni nuovo brano",
	"Notify DJ transitions":                         "Notifica le transizioni DJ",
	"A notification when the DJ Mixer starts a mix": "Una notifica quando il DJ Mixer inizia un mix",

	"Notify errors": "Notifica gli errori",

	"A notification when a track fails while the window is hidden": "Una notifica quando un brano non parte e la finestra è nascosta",

	"Silence and errors":                             "Silenzio ed errori",
	"Skip silence at the start":                      "Salta il silenzio iniziale",
	"Leading silence is cut before the track plays":  "Il silenzio iniziale viene tagliato prima che il brano parta",
	"Skip silence at the end":                        "Salta il silenzio finale",
	"The next track starts when the end goes silent": "Il brano successivo parte quando la fine diventa silenziosa",

	"Silence threshold": "Soglia del silenzio",

	"Lower values treat only true digital silence as silence; quiet intros are kept": "Valori più bassi considerano silenzio solo il silenzio digitale vero; le intro a basso volume restano",

	"Silence at the end lasts at least":     "Il silenzio finale dura almeno",
	"Shorter gaps are intentional pauses":   "Le pause più brevi sono intenzionali",
	"When a track fails":                    "Se un brano non parte",
	"Skip to the next track":                "Passa al successivo",
	"Retry once, then skip":                 "Riprova una volta, poi salta",
	"Automatic (adapts to the connection)":  "Automatica (si adatta alla connessione)",
	"High · Opus ~160 kbps":                 "Alta · Opus ~160 kbps",
	"Balanced · Opus ~70 kbps":              "Bilanciata · Opus ~70 kbps",
	"Data saver · Opus ~50 kbps":            "Risparmio dati · Opus ~50 kbps",
	"Measured speed: %.1f× real time":       "Velocità misurata: %.1f× il tempo reale",
	"The speed is measured while streaming": "La velocità viene misurata durante lo streaming",
	"Original (no conversion)":              "Originale (nessuna conversione)",
	"Streaming quality and network":         "Qualità di streaming e rete",
	"YouTube quality":                       "Qualità YouTube",

	"Opus gives the same quality as AAC with much less data": "Opus offre la stessa qualità di AAC con molti meno dati",

	"Read-ahead buffer": "Buffer di lettura anticipata",

	"More seconds keep slow or unstable connections playing": "Più secondi tengono in riproduzione le connessioni lente o instabili",

	"Download format": "Formato dei download",

	"MP3 and FLAC re-encode a lossy source and do not add quality": "MP3 e FLAC ricodificano una sorgente compressa e non aggiungono qualità",

	"Manual":     "Manuale",
	"Mix length": "Durata del mix",
	"Intensity":  "Intensità",

	"Smooth: soft blends · Beat Match: tight tempo sync · Long Mix: long layered mixes · Quick Mix: short cuts": "Smooth: mix morbidi · Beat Match: tempo sincronizzato · Long Mix: mix lunghi e stratificati · Quick Mix: passaggi brevi",
	"The length is rounded to whole bars at the tempo of the track, so every mix starts and ends on the beat.":  "La durata viene arrotondata a battute intere secondo il tempo del brano, così ogni mix inizia e finisce a tempo.",

	"Listen to the next transition":      "Ascolta la prossima transizione",
	"The transition starts in 6 seconds": "La transizione inizia tra 6 secondi",

	"Left":     "A sinistra",
	"Centered": "Centrato",
	"Gentle":   "Morbido",
	"Normal":   "Normale",
	"Snappy":   "Rapido",

	"Lyrics open by clicking the cover. Sources: tags of the file, the music service, LRCLIB (exact match by title, artist, album and duration) and NetEase.": "I testi si aprono cliccando la copertina. Fonti: tag del file, servizio musicale, LRCLIB (corrispondenza esatta per titolo, artista, album e durata) e NetEase.",

	"Text size": "Dimensione del testo",
	"Alignment": "Allineamento",
	"Scrolling": "Scorrimento",

	"How quickly the lyrics follow the song": "Quanto velocemente il testo segue la canzone",

	"Timing offset": "Anticipo",

	"Positive values show the lines earlier": "Valori positivi mostrano le righe prima",

	"From the cover": "Dalla copertina",
	"From the theme": "Dal tema",
	"Style":          "Stile",

	"NCS is the sphere of the NCS music videos": "NCS è la sfera dei video musicali NCS",

	"Sensitivity":                          "Sensibilità",
	"Raise it for quiet music":             "Aumentala per la musica a basso volume",
	"How strongly shapes and pulses react": "Quanto reagiscono forme e pulsazioni",

	"Colors": "Colori",

	"The visualizer stops drawing while it is hidden, so it costs nothing in the tray.": "Il visualizzatore smette di disegnare quando è nascosto, quindi nella tray non consuma nulla.",

	"Dynamic colors": "Colori dinamici",

	"The colors of the whole window follow the cover of the playing track": "I colori di tutta la finestra seguono la copertina del brano in riproduzione",

	"Your library":       "La tua libreria",
	"Your music servers": "I tuoi server musicali",
	"Sidebar":            "Barra laterale",

	"Services in the Library": "Servizi nella Libreria",
	"Reset the interface":     "Ripristina l'interfaccia",

	"Restore the theme, colors, layout, Home sections and sidebar to their defaults? Playback, DJ and service settings are kept.": "Ripristinare tema, colori, disposizione, sezioni della Home e barra laterale ai valori predefiniti? Le impostazioni di riproduzione, DJ e servizi restano invariate.",

	"Interface restored": "Interfaccia ripristinata",
	"Compact":            "Compatta",
	"Comfortable":        "Ariosa",
	"Personalization":    "Personalizzazione",
	"Density":            "Densità",

	"Space between the elements": "Spazio tra gli elementi",
	"Reduce motion":              "Riduci le animazioni",

	"Instant scrolling and color changes, fewer animations": "Scorrimento e cambi di colore istantanei, meno animazioni",

	"Home sections": "Sezioni della Home",

	"Tick the sections to show and order them with the arrows. Sections without content, such as a service you have not set up, never appear.": "Spunta le sezioni da mostrare e ordinale con le frecce. Le sezioni senza contenuto, come un servizio non configurato, non compaiono mai.",

	"Export settings…":      "Esporta impostazioni…",
	"Settings exported to ": "Impostazioni esportate in ",
	"Import settings…":      "Importa impostazioni…",

	"Replace the current settings with the ones exported on %s (Kalulu %s)? A backup of the current files is kept, then Kalulu restarts.": "Sostituire le impostazioni attuali con quelle esportate il %s (Kalulu %s)? Viene conservata una copia dei file attuali, poi Kalulu si riavvia.",

	"Import settings":   "Importa impostazioni",
	"Import and export": "Importazione ed esportazione",

	"One versioned file holds the Kalulu preferences and config.toml (services, EQ, plugins), to move them to another computer. Imported files are checked before they are applied.": "Un unico file versionato contiene le preferenze di Kalulu e config.toml (servizi, EQ, plugin), per trasferirli su un altro computer. I file importati vengono controllati prima di essere applicati.",

	"Not checked yet": "Non ancora controllato",
	"Checking…":       "Controllo in corso…",

	"No release has been published yet on GitHub.": "Non è ancora stato pubblicato nessun rilascio su GitHub.",

	"Could not check: ":                        "Controllo non riuscito: ",
	"Kalulu is up to date (latest: %s).":       "Kalulu è aggiornato (ultima versione: %s).",
	"Kalulu %s is available, published on %s.": "È disponibile Kalulu %s, pubblicato il %s.",

	"Updates":             "Aggiornamenti",
	"Installed version":   "Versione installata",
	"Check automatically": "Controlla automaticamente",

	"Once a day, in the background; playback is never interrupted": "Una volta al giorno, in background; la riproduzione non viene mai interrotta",

	"Check now":                                  "Controlla ora",
	"Open the release page":                      "Apri la pagina del rilascio",
	"Download and verify":                        "Scarica e verifica",
	"Downloading the update…":                    "Download dell'aggiornamento…",
	"Update verified and saved to ":              "Aggiornamento verificato e salvato in ",
	"Update installed; restarting…":              "Aggiornamento installato, riavvio…",
	"Kalulu %s is available: Settings → Updates": "È disponibile Kalulu %s: Impostazioni → Aggiornamenti",
	"Starting the audio engine":                  "Avvio del motore audio",
	"Loading your music sources":                 "Caricamento delle sorgenti musicali",
	"Loading the room's track…":                  "Caricamento del brano della stanza…",
	"In sync (%+.0f ms)":                         "In sincronia (%+.0f ms)",

	"Only the owner and the editors control the room": "Solo l'Owner e gli editor controllano la stanza",
	"Add to the room's queue":                         "Aggiungi alla coda della stanza",
	"Ask the owner to play it":                        "Chiedi all'Owner di riprodurlo",
	"Sent to the room":                                "Inviato alla stanza",
	"A guest added: ":                                 "Un ospite ha aggiunto: ",
	"Listen together":                                 "Ascolta insieme",

	"Share what you play with people on your local network, in sync.": "Condividi quello che ascolti con le persone della tua rete locale, in sincronia.",

	"%s's room":       "Stanza di %s",
	"Create the room": "Crea la stanza",

	"The port must be between 1024 and 65535": "La porta deve essere tra 1024 e 65535",
	"%s asks to play %s":                      "%s chiede di riprodurre %s",

	"Discovery is unavailable; guests can join with IP and port": "La ricerca automatica non è disponibile: gli ospiti possono entrare con IP e porta",

	"Create a room": "Crea una stanza",

	"You become the Owner: guests hear what you play and you decide what they may do.": "Diventi l'Owner: gli ospiti ascoltano ciò che riproduci e tu decidi cosa possono fare.",

	"Your name": "Il tuo nome",
	"Room name": "Nome della stanza",
	"Port":      "Porta",

	"TCP, and the next port in UDP for discovery":    "TCP, e la porta successiva in UDP per la ricerca",
	"The room code has 6 characters":                 "Il codice della stanza ha 6 caratteri",
	"Looking for the room…":                          "Ricerca della stanza…",
	"Enter the IP address and the port of the owner": "Inserisci l'indirizzo IP e la porta dell'Owner",

	"Room code":        "Codice stanza",
	"Find and join":    "Trova ed entra",
	"Owner IP address": "Indirizzo IP dell'Owner",

	"Enter the room code above too: it is the password of the room.": "Inserisci anche il codice della stanza qui sopra: è la password della stanza.",

	"Connect":             "Connetti",
	"Join a room":         "Entra in una stanza",
	"Or connect directly": "Oppure connettiti direttamente",

	"The room works on your local network only and is never exposed to the Internet. Everyone plays the same source on their own computer, so online tracks (YouTube, radio, podcasts) need each guest to reach them; your local files are streamed to the guests by Kalulu. If guests cannot find you, allow TCP and UDP on the room ports in your firewall.": "La stanza funziona solo nella tua rete locale e non è mai esposta su Internet. Ognuno riproduce la stessa sorgente sul proprio computer, quindi i brani online (YouTube, radio, podcast) devono essere raggiungibili da ogni ospite; i tuoi file locali vengono trasmessi agli ospiti da Kalulu. Se gli ospiti non ti trovano, consenti TCP e UDP sulle porte della stanza nel firewall.",

	"Owner":                           "Owner",
	"Editor":                          "Editor",
	"On request":                      "Su richiesta",
	"Listener":                        "Ascoltatore",
	"Close the room":                  "Chiudi la stanza",
	"Guests type it to find the room": "Gli ospiti lo digitano per trovare la stanza",

	"Address": "Indirizzo",

	"For a direct connection": "Per la connessione diretta",
	"No pending requests":     "Nessuna richiesta in attesa",

	"Editor: controls playback and the queue · On request: asks for tracks, you approve · Listener: listens only.": "Editor: controlla riproduzione e coda · Su richiesta: chiede brani, tu approvi · Ascoltatore: ascolta soltanto.",

	"People":              "Persone",
	"Requests":            "Richieste",
	"Leave the room":      "Esci dalla stanza",
	"Your role":           "Il tuo ruolo",
	"Sync":                "Sincronia",
	"Connected":           "Connesso",
	"Reconnecting…":       "Riconnessione…",
	"You left the room: ": "Sei uscito dalla stanza: ",
	"Together":            "Insieme",
	"Room open · code %s": "Stanza aperta · codice %s",
	"Balance":             "Bilanciamento",

	"Normalize the volume (same loudness for every track)": "Normalizza il volume (stesso livello per ogni brano)",

	"Performance":           "Prestazioni",
	"Home and services":     "Home e servizi",
	"DJ track analysis":     "Analisi brani DJ",
	"Desktop notifications": "Notifiche desktop",

	"All off by default. Kalulu never shows the same notification twice in a row.": "Tutte disattivate di default. Kalulu non mostra mai due volte di fila la stessa notifica.",
	"Equalizer, balance and normalization":                                         "Equalizzatore, bilanciamento e normalizzazione",

	"The equalizer, the L/R balance, the volume normalization and the mono output are on the Equalizer page.": "Equalizzatore, bilanciamento L/R, normalizzazione del volume e uscita mono sono nella pagina Equalizzatore.",

	"Downloads":        "Download",
	"Home and sidebar": "Home e barra laterale",
	"Covers":           "Copertine",

	"Kalulu pauses the visualizer, the lyrics animation and the polling when the window is hidden, and loads covers and Home sections only when they come on screen. To lighten it further, turn on Reduce motion in Personalization.": "Kalulu mette in pausa visualizer, animazione dei testi e aggiornamenti quando la finestra è nascosta, e carica copertine e sezioni della Home solo quando compaiono sullo schermo. Per alleggerirlo ancora, attiva Riduci animazioni in Personalizzazione.",

	"Cache cleared":                     "Cache svuotata",
	"Rebuilt automatically when needed": "Ricreata automaticamente quando serve",
	"Performance and cache":             "Prestazioni e cache",
	"Network":                           "Rete",
	"Notifications":                     "Notifiche",
	"Timing":                            "Sincronizzazione",
	"Saved tracks":                      "Brani salvati",
	"Folders":                           "Cartelle",

	"Remove the selected queue row": "Rimuovi la riga selezionata della coda",
	"Open folder":                   "Apri cartella",
	"Toggle the now playing panel":  "Mostra/nascondi il pannello In riproduzione",
	"Seek 30 s forward / back":      "Avanti / indietro di 30 s",
	"Volume up / down":              "Volume su / giù",
	"Toggle shuffle":                "Attiva/disattiva casuale",
	"Cycle repeat (off, all, one)":  "Cambia ripetizione (no, tutti, uno)",
	"Open files":                    "Apri file",
	"Open URL":                      "Apri URL",
	"Version":                       "Versione",
	"Space":                         "Spazio",
	"Play / pause":                  "Riproduci / pausa",
	"Next / previous track":         "Brano successivo / precedente",
	"Seek 5 s forward / back":       "Avanti / indietro di 5 s",

	"or":                    "o",
	"Toggle mono":           "Attiva/disattiva mono",
	"Next visualizer style": "Stile successivo del visualizer",

	"Delete":                          "Canc",
	"Speed down / up":                 "Velocità giù / su",
	"Full screen visualizer / window": "Visualizer / finestra a schermo intero",
	"Queue, Search, Player and lyrics, Equalizer, History": "Coda, Cerca, Player e testi, Equalizzatore, Cronologia",

	"This help": "Questa guida",
	"Close":     "Chiudi",

	"A modern graphical music player, forked from cliamp — the retro terminal player by Bjarne Øverli. Local files, streams, internet radio, podcasts, Spotify, Tidal, Qobuz, YouTube Music, SoundCloud, Navidrome, Jellyfin, Plex, Emby and more, with a 10-band EQ, synced lyrics, visualizers and Lua plugins.": "Un lettore musicale grafico e moderno, fork di cliamp, il lettore da terminale di Bjarne Øverli. File locali, stream, radio, podcast, Spotify, Tidal, Qobuz, YouTube Music, SoundCloud, Navidrome, Jellyfin, Plex, Emby e altro, con equalizzatore a 10 bande, testi sincronizzati, visualizer e plugin Lua.",

	"Double-click: next style · F: full screen · Esc: close": "Doppio clic: stile successivo · F: schermo intero · Esc: chiudi",
	"Lyrics earlier (−0.25 s)":                               "Testo in anticipo (−0,25 s)",
	"Reset the offset":                                       "Azzera l'offset",
	"Lyrics offset reset":                                    "Offset dei testi azzerato",
	"Synced lyrics":                                          "Testo sincronizzato",
	"Lyrics offset %+.2f s":                                  "Offset testi %+.2f s",

	"Source":                    "Fonte",
	"Offset: %+.2f s":           "Offset: %+.2f s",
	"Look for the lyrics again": "Cerca di nuovo il testo",
	"Lyrics later (+0.25 s)":    "Testo in ritardo (+0,25 s)",

	"No lyrics found · ⚙ to look again or pick another source": "Testo non trovato · ⚙ per riprovare o scegliere un'altra fonte",
	"Lyrics synced word by word":                               "Testo sincronizzato parola per parola",
	"Automatic (best of all)":                                  "Automatica (la migliore tra tutte)",
}
