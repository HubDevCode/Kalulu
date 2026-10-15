# Kalulu — lista di lavoro

Trasformazione di GuiAmp in **Kalulu**. Ogni voce viene spuntata solo dopo
una verifica reale (test automatici, prova sul motore o anteprima della
finestra). Le note "Decisione" spiegano le scelte prese dove la richiesta
lascia aperta un'alternativa.

Legenda: `[x]` fatto e verificato · `[~]` parziale · `[ ]` da fare

## Fase 1 — Fondamenta

### 16. Rebranding GuiAmp → Kalulu
- [x] Nome applicazione, binario `kalulu`, titolo finestra, tray, notifiche, menu
- [x] Logo da `~/Scaricati/logo512.png` incluso nel binario (non dipende dal percorso)
- [x] Icone per menu applicazioni, AppImage, Flatpak, metainfo
- [x] Impostazioni → "Kalulu (ex GuiAmp)"
- [x] Preferenze migrate dal vecchio ID `stream.guiamp.GuiAmp`
- Decisione: la cartella `~/.config/cliamp` resta quella del motore (compatibilità
  con cliamp); le preferenze proprie di Kalulu sono in `~/.config/fyne/io.kalulu.Kalulu`
  e le cache in `~/.cache/kalulu`.

### 9. Notifiche e istanza singola
- [x] Notifiche disattivate di default, attivabili per evento (cambio brano, transizione DJ, errori)
- [x] Istanza singola: un secondo avvio riporta in primo piano la finestra e le passa i file/URL
- [x] Avvio in background (`kalulu --background`) direttamente nella tray

### 1. Silenzio ed errori
- [x] Skip del silenzio iniziale e finale di ogni brano, anche in modalità gapless
- [x] Rilevamento a soglia + durata minima, distinto da intro a basso volume
- [x] Impostazioni: on/off, sensibilità, inizio e fine separati
- [x] Brano che non si carica → passa al successivo (con impostazione: salta / riprova / ferma)

### 11. Controlli audio
- [x] Togliere Stereo/Mono dalla barra inferiore; resta in Equalizzatore e Impostazioni
- [ ] Normalizzazione volume e bilanciamento L/R nel motore

## Fase 2 — Audio e streaming

### 2. Caricamento e qualità
- [x] YouTube: formato Opus (alta qualità a ~130–160 kbps) invece del "migliore" generico
- [x] Profili di qualità: Alta / Bilanciata / Risparmio dati / Automatica (adattiva)
- [x] Buffer di rete più ampio e configurabile per connessioni lente
- [x] Analisi DJ con il formato più leggero (serve solo per BPM/tonalità)
- [x] Cache delle copertine su disco con miniature
- [x] Download: formato configurabile (originale / Opus / MP3 / FLAC) e cartella
- Decisione: nessuna conversione con perdita se il formato originale va bene.

### 3. DJ Mixer
- [x] Durata della transizione con slider (secondi, convertiti in battute a seconda dei BPM)
- [x] Preset: Smooth, Beat Match, Long Mix, Quick Mix, Manuale
- [x] Intensità (EQ/filtri), anteprima della transizione (salto a 6 s prima del mix)
- [x] Unica schermata DJ Mixer (le opzioni non sono duplicate altrove)

## Fase 3 — Esperienza

### 4. Lyrics
- [x] Un'unica schermata avanzata: si apre cliccando la copertina; pulsante schermo intero
- [x] Evidenziazione parola per parola, scorrimento animato, righe vicine sfumate
- [x] Più fonti con fallback: LRCLIB, NetEase, testi incorporati, provider (fonti di cliamp)
  + ricerca con titolo/artista/album/durata per evitare associazioni sbagliate
- [x] Personalizzazione: dimensione, allineamento, animazioni, offset

### 5. Visualizer
- [x] Sfera NCS a tempo (rilevamento beat sui bassi, particelle, alone)
- [x] Nuovi stili e intensità/sensibilità/colori configurabili
- [x] Schermo intero; si ferma quando la finestra è nascosta

### 6–7. Interfaccia, colori dinamici, personalizzazione
- [x] Palette dinamica dalla copertina con contrasto garantito e transizione graduale
- [ ] Icone riviste
- [x] Personalizzazione: sezioni barra laterale, densità, dimensione testo, animazioni, pannelli
- [x] Ripristino predefiniti (con conferma)

### 8. Home modulare
- [x] Scelta dei servizi e delle sezioni della Home, ordine e servizio predefinito
- [x] Nessuna sezione vuota per servizi non configurati

### 10. Impostazioni centralizzate
- [ ] Categorie richieste, un solo punto per ogni opzione
- [x] Esportazione/importazione versionata e validata (preferenze Kalulu + config.toml)

### 12–13. Caricamento e prestazioni
- [x] Schermata di avvio con lo stato reale delle operazioni
- [x] Home e immagini caricate quando diventano visibili
- [x] Animazioni e polling ridotti quando la finestra è nascosta

## Fase 4 — Rete

### 15. Aggiornamenti da GitHub (HubDevCode/GuiAmp)
- [x] Controllo manuale e automatico, versione/data/note, apertura della pagina del rilascio
- [x] Verifica di provenienza e checksum prima dell'installazione

### 14. Ascolto condiviso in LAN
- [ ] Stanza con codice (scoperta in LAN via broadcast UDP) o IP:porta
- [ ] Ruoli Owner / Editor / Su richiesta / Ascoltatore, modificabili a sessione aperta
- [ ] Sincronizzazione brano, posizione, pausa; correzione graduale della deriva
- [ ] Richieste di brani da approvare
- Decisione: ogni client riproduce autonomamente la stessa sorgente (YouTube,
  radio, podcast, URL). Per i file locali dell'Owner, l'Owner li serve in HTTP
  sulla LAN agli ospiti autenticati. Niente esposizione su Internet.

## Fase 5 — Pulizia e verifica finale
- [ ] Rimozione di schermate e controlli duplicati (pagina Testi separata, mono nella barra)
- [ ] Documentazione, test, criteri di completamento della sezione 19
