package discover

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Podcast is a show of the Apple podcast directory.
type Podcast struct {
	ID       string
	Title    string
	Author   string
	FeedURL  string
	Artwork  string
	Genre    string
	Episodes int
}

// PodcastGenre is a category of the Apple charts.
type PodcastGenre struct {
	ID   string
	Name string
}

// PodcastGenres are the main podcast categories.
var PodcastGenres = []PodcastGenre{
	{"1489", "News"}, {"1303", "Comedy"}, {"1324", "Society & Culture"}, {"1488", "True Crime"},
	{"1487", "History"}, {"1318", "Technology"}, {"1533", "Science"}, {"1321", "Business"},
	{"1512", "Health & Fitness"}, {"1545", "Sports"}, {"1310", "Music"}, {"1309", "TV & Film"},
	{"1301", "Arts"}, {"1304", "Education"}, {"1483", "Fiction"}, {"1305", "Kids & Family"},
	{"1502", "Leisure"}, {"1314", "Religion & Spirituality"},
}

var podcastHTTP = &http.Client{Timeout: 20 * time.Second}

// TopPodcasts returns the podcast chart of country, or of one genre of it
// when genreID is set.
func TopPodcasts(ctx context.Context, country, genreID string) ([]Podcast, error) {
	country = strings.ToLower(strings.TrimSpace(country))
	if len(country) != 2 {
		country = "us"
	}
	var ids []string
	if genreID == "" {
		var chart struct {
			Feed struct {
				Results []struct {
					ID string `json:"id"`
				} `json:"results"`
			} `json:"feed"`
		}
		u := "https://rss.marketingtools.apple.com/api/v2/" + country + "/podcasts/top/50/podcasts.json"
		if err := getJSON(ctx, u, &chart); err != nil {
			return nil, fmt.Errorf("podcast chart: %w", err)
		}
		for _, r := range chart.Feed.Results {
			ids = append(ids, r.ID)
		}
	} else {
		var chart struct {
			Feed struct {
				Entry []struct {
					ID struct {
						Attributes struct {
							ID string `json:"im:id"`
						} `json:"attributes"`
					} `json:"id"`
				} `json:"entry"`
			} `json:"feed"`
		}
		u := "https://itunes.apple.com/" + country + "/rss/toppodcasts/limit=50/genre=" + url.PathEscape(genreID) + "/json"
		if err := getJSON(ctx, u, &chart); err != nil {
			return nil, fmt.Errorf("podcast chart: %w", err)
		}
		for _, e := range chart.Feed.Entry {
			ids = append(ids, e.ID.Attributes.ID)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	shows, err := lookupPodcasts(ctx, url.Values{"id": {strings.Join(ids, ",")}, "entity": {"podcast"}, "country": {country}})
	if err != nil {
		return nil, err
	}
	byID := map[string]Podcast{}
	for _, s := range shows {
		byID[s.ID] = s
	}
	var ranked []Podcast
	for _, id := range ids {
		if s, ok := byID[id]; ok {
			ranked = append(ranked, s)
		}
	}
	return ranked, nil
}

// SearchPodcasts finds shows by name.
func SearchPodcasts(ctx context.Context, term, country string) ([]Podcast, error) {
	term = strings.TrimSpace(term)
	if term == "" {
		return nil, nil
	}
	params := url.Values{"term": {term}, "media": {"podcast"}, "entity": {"podcast"}, "limit": {"50"}}
	if len(country) == 2 {
		params.Set("country", strings.ToLower(country))
	}
	return lookupPodcasts(ctx, params, "search")
}

func lookupPodcasts(ctx context.Context, params url.Values, endpoint ...string) ([]Podcast, error) {
	ep := "lookup"
	if len(endpoint) > 0 {
		ep = endpoint[0]
	}
	var resp struct {
		Results []struct {
			CollectionID     json.Number `json:"collectionId"`
			CollectionName   string      `json:"collectionName"`
			ArtistName       string      `json:"artistName"`
			FeedURL          string      `json:"feedUrl"`
			ArtworkURL600    string      `json:"artworkUrl600"`
			ArtworkURL100    string      `json:"artworkUrl100"`
			PrimaryGenreName string      `json:"primaryGenreName"`
			TrackCount       int         `json:"trackCount"`
		} `json:"results"`
	}
	if err := getJSON(ctx, "https://itunes.apple.com/"+ep+"?"+params.Encode(), &resp); err != nil {
		return nil, fmt.Errorf("podcasts: %w", err)
	}
	var out []Podcast
	for _, r := range resp.Results {
		if !strings.HasPrefix(r.FeedURL, "http") {
			continue
		}
		art := r.ArtworkURL600
		if art == "" {
			art = r.ArtworkURL100
		}
		out = append(out, Podcast{
			ID: r.CollectionID.String(), Title: r.CollectionName, Author: r.ArtistName,
			FeedURL: r.FeedURL, Artwork: art, Genre: r.PrimaryGenreName, Episodes: r.TrackCount,
		})
	}
	return out, nil
}

func getJSON(ctx context.Context, u string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Kalulu")
	resp, err := podcastHTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 8<<20)).Decode(out)
}
