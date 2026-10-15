package discover

import (
	"encoding/json"
	"testing"
)

func TestParseDuration(t *testing.T) {
	tests := map[string]int{"3:45": 225, "1:02:03": 3723, "45": 0, "": 0, "a:b": 0, " 4:00 ": 240}
	for in, want := range tests {
		if got := parseDuration(in); got != want {
			t.Errorf("parseDuration(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestApplyEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		kind     string
		id       string
	}{
		{"song", `{"watchEndpoint":{"videoId":"abc"}}`, KindSong, "abc"},
		{"video", `{"watchEndpoint":{"videoId":"abc","watchEndpointMusicSupportedConfigs":{"watchEndpointMusicConfig":{"musicVideoType":"MUSIC_VIDEO_TYPE_UGC"}}}}`, KindVideo, "abc"},
		{"album", `{"browseEndpoint":{"browseId":"MPREb_x","browseEndpointContextSupportedConfigs":{"browseEndpointContextMusicConfig":{"pageType":"MUSIC_PAGE_TYPE_ALBUM"}}}}`, KindAlbum, "MPREb_x"},
		{"artist by prefix", `{"browseEndpoint":{"browseId":"UCxyz"}}`, KindArtist, "UCxyz"},
		{"playlist by prefix", `{"browseEndpoint":{"browseId":"VLPL1"}}`, KindPlaylist, "VLPL1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ep any
			if err := json.Unmarshal([]byte(tt.endpoint), &ep); err != nil {
				t.Fatal(err)
			}
			var it Item
			applyEndpoint(&it, ep)
			id := it.VideoID
			if id == "" {
				id = it.BrowseID
			}
			if it.Kind != tt.kind || id != tt.id {
				t.Fatalf("got kind %q id %q, want %q %q", it.Kind, id, tt.kind, tt.id)
			}
		})
	}
}

const shelfFixture = `{"contents":[{"musicCarouselShelfRenderer":{
  "header":{"musicCarouselShelfBasicHeaderRenderer":{"title":{"runs":[{"text":"Quick picks"}]}}},
  "contents":[
    {"musicResponsiveListItemRenderer":{
      "thumbnail":{"musicThumbnailRenderer":{"thumbnail":{"thumbnails":[{"url":"https://x/s.jpg","width":60},{"url":"https://x/l.jpg","width":120}]}}},
      "playlistItemData":{"videoId":"v1"},
      "flexColumns":[
        {"musicResponsiveListItemFlexColumnRenderer":{"text":{"runs":[{"text":"Song One","navigationEndpoint":{"watchEndpoint":{"videoId":"v1"}}}]}}},
        {"musicResponsiveListItemFlexColumnRenderer":{"text":{"runs":[
          {"text":"Artist A","navigationEndpoint":{"browseEndpoint":{"browseId":"UCa","browseEndpointContextSupportedConfigs":{"browseEndpointContextMusicConfig":{"pageType":"MUSIC_PAGE_TYPE_ARTIST"}}}}},
          {"text":" • "},
          {"text":"Album B","navigationEndpoint":{"browseEndpoint":{"browseId":"MPREb_b","browseEndpointContextSupportedConfigs":{"browseEndpointContextMusicConfig":{"pageType":"MUSIC_PAGE_TYPE_ALBUM"}}}}},
          {"text":" • "},{"text":"3:20"}]}}}]}},
    {"musicTwoRowItemRenderer":{
      "title":{"runs":[{"text":"Mix"}]},
      "subtitle":{"runs":[{"text":"Playlist"}]},
      "navigationEndpoint":{"browseEndpoint":{"browseId":"VLRD1"}},
      "thumbnailRenderer":{"musicThumbnailRenderer":{"thumbnail":{"thumbnails":[{"url":"https://lh3.googleusercontent.com/a=w120-h120","width":120}]}}}}}
  ]}}]}`

func TestCollectShelves(t *testing.T) {
	var data any
	if err := json.Unmarshal([]byte(shelfFixture), &data); err != nil {
		t.Fatal(err)
	}
	shelves := collectShelves(data)
	if len(shelves) != 1 || shelves[0].Title != "Quick picks" || len(shelves[0].Items) != 2 {
		t.Fatalf("shelves = %+v", shelves)
	}
	song, mix := shelves[0].Items[0], shelves[0].Items[1]
	if song.Kind != KindSong || song.VideoID != "v1" || song.Artist != "Artist A" || song.Album != "Album B" || song.Duration != 200 || song.Thumb != "https://x/l.jpg" {
		t.Errorf("song = %+v", song)
	}
	if mix.Kind != KindPlaylist || mix.BrowseID != "VLRD1" || mix.Thumb != "https://lh3.googleusercontent.com/a=w544-h544" {
		t.Errorf("mix = %+v", mix)
	}
	if got := song.WatchURL(); got != "https://music.youtube.com/watch?v=v1" {
		t.Errorf("WatchURL = %q", got)
	}
}
