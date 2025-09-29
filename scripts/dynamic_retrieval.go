package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/joho/godotenv"
)

type harvestSeeds struct {
	Keywords []string     `json:"keywords"`
	Genres   []string     `json:"genres"`
	Moods    []string     `json:"moods"`
	Meta     []string     `json:"meta"`
	Locales  []string     `json:"locales"`
	Artists  []seedArtist `json:"artists"`
	Tracks   []seedTrack  `json:"tracks"`
}

type seedArtist struct {
	Name        string   `json:"name"`
	Aliases     []string `json:"aliases"`
	SpotifyID   string   `json:"spotify_id"`
	TopTrackIDs []string `json:"top_track_ids"`
}

type seedTrack struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type seedQuery struct {
	Query  string
	Source string
}

type playlistDetail struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	Public        bool   `json:"public"`
	Collaborative bool   `json:"collaborative"`
	Owner         struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
	} `json:"owner"`
	SnapshotID string `json:"snapshot_id"`
	Followers  struct {
		Total int `json:"total"`
	} `json:"followers"`
	Images []struct {
		URL    string `json:"url"`
		Height int    `json:"height"`
		Width  int    `json:"width"`
	} `json:"images"`
	Tracks struct {
		Total int `json:"total"`
	} `json:"tracks"`
}

type playlistSimple struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Owner       struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
	} `json:"owner"`
	SnapshotID string `json:"snapshot_id"`
	Tracks     struct {
		Total int `json:"total"`
	} `json:"tracks"`
}

type playlistPage struct {
	Items []playlistSimple `json:"items"`
	Next  string           `json:"next"`
}

type searchResponse struct {
	Playlists playlistPage `json:"playlists"`
}

type playlistPageResponse struct {
	Playlists playlistPage `json:"playlists"`
}

type featuredResponse struct {
	Message   string       `json:"message"`
	Playlists playlistPage `json:"playlists"`
}

type categoryList struct {
	Categories struct {
		Items []category `json:"items"`
		Next  string     `json:"next"`
	} `json:"categories"`
}

type category struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type playlistTracksPage struct {
	Items []playlistTrackItem `json:"items"`
	Next  string              `json:"next"`
}

type playlistTrackItem struct {
	AddedAt string `json:"added_at"`
	AddedBy struct {
		ID string `json:"id"`
	} `json:"added_by"`
	Track struct {
		ID           string            `json:"id"`
		Name         string            `json:"name"`
		URI          string            `json:"uri"`
		ExternalUrls map[string]string `json:"external_urls"`
		Artists      []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"artists"`
		Album struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"album"`
	} `json:"track"`
}

type harvestOrigin struct {
	Source string
	Query  string
}

type relevanceResult struct {
	Score          float64
	KeywordMatches []string
	ArtistMatches  []string
	TrackMatches   []string
	FreshnessDays  int
	FollowerBoost  float64
	FreshnessBoost float64
}

type seedIndex struct {
	keywords     []string
	artistByName map[string]seedArtist
	artistByID   map[string]seedArtist
	trackByID    map[string]seedTrack
}

type csvStore struct {
	file   *os.File
	writer *csv.Writer
	mu     sync.Mutex
}

type snapshotCache struct {
	path  string
	mu    sync.Mutex
	data  map[string]string
	dirty bool
}

type rateLimiter struct {
	ticker   *time.Ticker
	interval time.Duration
}

type spotifyClient struct {
	rest    *resty.Client
	limiter *rateLimiter
}

type harvester struct {
	client    *spotifyClient
	seeds     *harvestSeeds
	index     *seedIndex
	playlists *csvStore
	tracks    *csvStore
	snapshots *snapshotCache
	seen      map[string]struct{}
	mu        sync.Mutex
	opts      harvestOptions
}

type harvestOptions struct {
	ScoreThreshold    float64
	MaxSearchPages    int
	MaxBrowsePages    int
	IncludeFeatured   bool
	IncludeCategories bool
}

func main() {
	envFile := flag.String("env", ".env", "Path to .env file with Spotify credentials")
	playlistOut := flag.String("playlist-out", "data/playlists_dynamic.csv", "CSV to persist harvested playlists")
	trackOut := flag.String("track-out", "data/playlist_tracks_dynamic.csv", "CSV to persist harvested playlist tracks")
	seedFile := flag.String("seeds", "", "Optional JSON file with custom seed configuration")
	stateFile := flag.String("state", "data/dynamic_snapshot_state.json", "Path to snapshot cache file")
	maxSearchPages := flag.Int("max-search-pages", 6, "Max search result pages per seed query (each page = 50 playlists)")
	maxBrowsePages := flag.Int("max-browse-pages", 2, "Max browse pages per category/featured list")
	scoreThreshold := flag.Float64("score-threshold", 2.5, "Minimum relevance score required to persist playlist data")
	rateLimit := flag.Float64("rate-limit", 7.5, "Max Spotify API requests per second")
	includeFeatured := flag.Bool("include-featured", true, "Include Spotify featured playlists pass")
	includeCategories := flag.Bool("include-categories", true, "Include Spotify browse categories pass")
	flag.Parse()

	if err := godotenv.Load(*envFile); err != nil {
		log.Fatalf("failed to load env file %s: %v", *envFile, err)
	}

	clientID := strings.TrimSpace(os.Getenv("CLIENT_ID"))
	clientSecret := strings.TrimSpace(os.Getenv("CLIENT_SECRET"))
	if clientID == "" || clientSecret == "" {
		log.Fatal("CLIENT_ID and CLIENT_SECRET must be present in the environment")
	}

	token, err := requestClientCredentialsToken(clientID, clientSecret)
	if err != nil {
		log.Fatalf("failed to request Spotify token: %v", err)
	}

	seeds, err := loadSeedsWithFallback(*seedFile)
	if err != nil {
		log.Fatalf("failed to load seeds: %v", err)
	}

	queries := generateSeedQueries(seeds)
	log.Printf("generated %d seed queries", len(queries))

	playlistStore, err := newCSVStore(*playlistOut, []string{"playlist_id", "name", "description", "followers", "public", "collaborative", "owner_id", "owner_name", "origin", "query", "score", "matched_keywords", "matched_artists", "matched_tracks", "snapshot_id", "image_url", "track_total", "freshness_days", "last_refreshed_at"})
	if err != nil {
		log.Fatalf("failed to open playlist CSV: %v", err)
	}
	defer func() {
		if cerr := playlistStore.Close(); cerr != nil {
			log.Printf("close playlist CSV: %v", cerr)
		}
	}()

	trackStore, err := newCSVStore(*trackOut, []string{"playlist_id", "track_id", "track_name", "artists", "album_id", "added_at", "added_by", "external_url", "origin", "query"})
	if err != nil {
		log.Fatalf("failed to open track CSV: %v", err)
	}
	defer func() {
		if cerr := trackStore.Close(); cerr != nil {
			log.Printf("close track CSV: %v", cerr)
		}
	}()

	snapshotCache, err := loadSnapshotCache(*stateFile)
	if err != nil {
		log.Fatalf("failed to load snapshot cache: %v", err)
	}
	defer func() {
		if err := snapshotCache.Save(); err != nil {
			log.Printf("snapshot cache save failed: %v", err)
		}
	}()

	client := newSpotifyClient(token, *rateLimit)
	defer client.Close()

	opts := harvestOptions{
		ScoreThreshold:    *scoreThreshold,
		MaxSearchPages:    *maxSearchPages,
		MaxBrowsePages:    *maxBrowsePages,
		IncludeFeatured:   *includeFeatured,
		IncludeCategories: *includeCategories,
	}

	h := newHarvester(client, seeds, playlistStore, trackStore, snapshotCache, opts)

	ctx := context.Background()

	if err := h.harvestSearch(ctx, queries); err != nil {
		log.Printf("search harvest encountered errors: %v", err)
	}

	if opts.IncludeFeatured {
		if err := h.harvestFeatured(ctx); err != nil {
			log.Printf("featured harvest encountered errors: %v", err)
		}
	}

	if opts.IncludeCategories {
		if err := h.harvestCategories(ctx); err != nil {
			log.Printf("category harvest encountered errors: %v", err)
		}
	}
}

func loadSeedsWithFallback(path string) (*harvestSeeds, error) {
	defaults := defaultHarvestSeeds()
	if path == "" {
		return defaults, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read seeds file: %w", err)
	}

	var custom harvestSeeds
	if err := json.Unmarshal(data, &custom); err != nil {
		return nil, fmt.Errorf("parse seeds file: %w", err)
	}

	merged := mergeSeeds(defaults, &custom)
	return merged, nil
}

func defaultHarvestSeeds() *harvestSeeds {
	return &harvestSeeds{
		Keywords: []string{
			"fresh finds", "viral", "underground", "discover", "editorial", "playlist", "mix",
		},
		Genres: []string{
			"rock", "indie pop", "techno", "k-pop", "lofi", "hip hop", "latin", "afrobeat", "r&b", "country", "metal", "soul", "jazz", "ambient", "house", "edm",
		},
		Moods: []string{
			"study", "focus", "workout", "party", "chill", "sleep", "meditation", "running", "coding", "gaming", "summer", "relax", "road trip", "dance",
		},
		Meta: []string{
			"best", "top", "hits", "essentials", "throwback", "2024", "new", "fresh", "daily", "ultimate",
		},
		Locales: []string{
			"deutsch", "español", "français", "日本語", "한국어", "latino", "português", "brazil", "italiano", "हिन्दी", "العربية",
		},
		Artists: []seedArtist{
			{
				Name:        "Taylor Swift",
				Aliases:     []string{"taylor swift", "tay tay"},
				SpotifyID:   "06HL4z0CvFAxyc27GXpf02",
				TopTrackIDs: []string{"06AKEBrKUckW0KREUWRnvT", "2Cy7UlvJXf6xLTxpIi1D2n"},
			},
			{
				Name:        "Bad Bunny",
				Aliases:     []string{"bad bunny", "conejo malo"},
				SpotifyID:   "4q3ewBCX7sLwd24euuV69X",
				TopTrackIDs: []string{"0LcJLqbBmaGUft1e9Mm8HV", "5CnpZV3q5BcESefcB3WJmz"},
			},
			{
				Name:        "BTS",
				Aliases:     []string{"bts", "bangtan"},
				SpotifyID:   "3Nrfpe0tUJi4K4DXYWgMUX",
				TopTrackIDs: []string{"0e7ipj03S05BNilyu5bRzt", "62vpWI1CHwFy7tMIcSStl8"},
			},
			{
				Name:        "Billie Eilish",
				Aliases:     []string{"billie eilish"},
				SpotifyID:   "6qqNVTkY8uBg9cP3Jd7DAH",
				TopTrackIDs: []string{"4RVwu0g32PAqgUiJoXsdF8", "2Fxmhks0bxGSBdJ92vM42m"},
			},
			{
				Name:        "Drake",
				Aliases:     []string{"drake"},
				SpotifyID:   "3TVXtAsR1Inumwj472S9r4",
				TopTrackIDs: []string{"7KXjTSCq5nL1LoYtL7XAwS", "79LJU0YJXD8m1iS8q6fX3U"},
			},
		},
		Tracks: []seedTrack{
			{ID: "11dFghVXANMlKmJXsNCbNl", Name: "Blinding Lights"},
			{ID: "2XqCY74pdjpxwx1rsYc5Hm", Name: "Dance The Night"},
			{ID: "3ZCTVFBt2Brf31RLEnCkWJ", Name: "Flowers"},
			{ID: "0Q9ioqmbzEKx0G8Zb2m8RA", Name: "As It Was"},
			{ID: "2Fxmhks0bxGSBdJ92vM42m", Name: "Bad Guy"},
		},
	}
}

func mergeSeeds(base, override *harvestSeeds) *harvestSeeds {
	result := &harvestSeeds{}
	result.Keywords = mergeStringSlices(base.Keywords, override.Keywords)
	result.Genres = mergeStringSlices(base.Genres, override.Genres)
	result.Moods = mergeStringSlices(base.Moods, override.Moods)
	result.Meta = mergeStringSlices(base.Meta, override.Meta)
	result.Locales = mergeStringSlices(base.Locales, override.Locales)
	result.Artists = mergeArtists(base.Artists, override.Artists)
	result.Tracks = mergeTracks(base.Tracks, override.Tracks)
	return result
}

func mergeStringSlices(base, extra []string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(base)+len(extra))
	for _, v := range base {
		normalized := strings.TrimSpace(v)
		if normalized == "" {
			continue
		}
		key := strings.ToLower(normalized)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, normalized)
	}
	for _, v := range extra {
		normalized := strings.TrimSpace(v)
		if normalized == "" {
			continue
		}
		key := strings.ToLower(normalized)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, normalized)
	}
	return out
}

func mergeArtists(base, extra []seedArtist) []seedArtist {
	merged := make(map[string]seedArtist)
	order := make([]string, 0, len(base)+len(extra))
	add := func(a seedArtist) {
		key := a.SpotifyID
		if key == "" {
			key = strings.ToLower(strings.TrimSpace(a.Name))
		}
		existing, ok := merged[key]
		if !ok {
			merged[key] = a
			order = append(order, key)
			return
		}
		existing.Aliases = mergeStringSlices(existing.Aliases, a.Aliases)
		existing.TopTrackIDs = mergeStringSlices(existing.TopTrackIDs, a.TopTrackIDs)
		if existing.Name == "" {
			existing.Name = a.Name
		}
		if existing.SpotifyID == "" {
			existing.SpotifyID = a.SpotifyID
		}
		merged[key] = existing
	}
	for _, a := range base {
		add(a)
	}
	for _, a := range extra {
		add(a)
	}
	result := make([]seedArtist, 0, len(order))
	for _, key := range order {
		result = append(result, merged[key])
	}
	return result
}

func mergeTracks(base, extra []seedTrack) []seedTrack {
	merged := make(map[string]seedTrack)
	order := make([]string, 0, len(base)+len(extra))
	add := func(t seedTrack) {
		key := t.ID
		if key == "" {
			key = strings.ToLower(strings.TrimSpace(t.Name))
		}
		if key == "" {
			return
		}
		if _, ok := merged[key]; !ok {
			merged[key] = t
			order = append(order, key)
		}
	}
	for _, t := range base {
		add(t)
	}
	for _, t := range extra {
		add(t)
	}
	result := make([]seedTrack, 0, len(order))
	for _, key := range order {
		result = append(result, merged[key])
	}
	return result
}

func generateSeedQueries(seeds *harvestSeeds) []seedQuery {
	seen := make(map[string]struct{})
	result := make([]seedQuery, 0, 512)
	add := func(text, source string) {
		trimmed := strings.TrimSpace(text)
		if trimmed == "" {
			return
		}
		key := strings.ToLower(trimmed)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		result = append(result, seedQuery{Query: trimmed, Source: source})
	}

	for _, kw := range seeds.Keywords {
		add(kw, "keyword")
	}
	for _, genre := range seeds.Genres {
		add(genre, "genre")
	}
	for _, mood := range seeds.Moods {
		add(mood, "mood")
	}
	for _, locale := range seeds.Locales {
		add(locale, "locale")
	}
	for _, meta := range seeds.Meta {
		add(meta, "meta")
	}

	for _, mood := range seeds.Moods {
		for _, genre := range seeds.Genres {
			add(fmt.Sprintf("%s %s", mood, genre), "mood+genre")
		}
	}

	for _, locale := range seeds.Locales {
		for _, genre := range seeds.Genres {
			add(fmt.Sprintf("%s %s", locale, genre), "locale+genre")
		}
	}

	for _, meta := range seeds.Meta {
		for _, genre := range seeds.Genres {
			add(fmt.Sprintf("%s %s", meta, genre), "meta+genre")
		}
	}

	for _, artist := range seeds.Artists {
		add(artist.Name, "artist")
		add(fmt.Sprintf("%s best", artist.Name), "artist-meta")
		add(fmt.Sprintf("%s hits", artist.Name), "artist-meta")
		for _, alias := range artist.Aliases {
			add(alias, "artist-alias")
			add(fmt.Sprintf("%s hits", alias), "artist-alias")
		}
	}

	for _, track := range seeds.Tracks {
		add(track.Name, "track")
		add(fmt.Sprintf("%s playlist", track.Name), "track")
	}

	return result
}

func newSpotifyClient(token string, rate float64) *spotifyClient {
	if rate <= 0 {
		rate = 5.0
	}
	client := resty.New()
	client.SetBaseURL("https://api.spotify.com")
	client.SetAuthToken(token)
	client.SetHeader("Accept", "application/json")
	client.SetTimeout(30 * time.Second)
	rl := newRateLimiter(rate)
	return &spotifyClient{rest: client, limiter: rl}
}

func (c *spotifyClient) Close() {
	if c.limiter != nil {
		c.limiter.Stop()
	}
}

func (c *spotifyClient) searchPlaylists(ctx context.Context, query string, offset int) (playlistPage, error) {
	req := c.rest.R().
		SetContext(ctx).
		SetQueryParams(map[string]string{
			"q":      query,
			"type":   "playlist",
			"limit":  "50",
			"offset": strconv.Itoa(offset),
		})
	resp, err := c.execute(ctx, req, http.MethodGet, "/v1/search")
	if err != nil {
		return playlistPage{}, err
	}
	var payload searchResponse
	if err := json.Unmarshal(resp.Body(), &payload); err != nil {
		return playlistPage{}, err
	}
	return payload.Playlists, nil
}

func (c *spotifyClient) getPlaylist(ctx context.Context, playlistID string) (*playlistDetail, error) {
	fields := "id,name,description,public,collaborative,followers.total,owner(id,display_name),snapshot_id,images(url,height,width),tracks.total"
	path := fmt.Sprintf("/v1/playlists/%s", url.PathEscape(playlistID))
	req := c.rest.R().
		SetContext(ctx).
		SetQueryParam("fields", fields)
	resp, err := c.execute(ctx, req, http.MethodGet, path)
	if err != nil {
		return nil, err
	}
	var detail playlistDetail
	if err := json.Unmarshal(resp.Body(), &detail); err != nil {
		return nil, err
	}
	return &detail, nil
}

func (c *spotifyClient) getPlaylistTracks(ctx context.Context, playlistID string) ([]playlistTrackItem, error) {
	all := make([]playlistTrackItem, 0, 128)
	offset := 0
	for {
		req := c.rest.R().
			SetContext(ctx).
			SetQueryParams(map[string]string{
				"limit":  "100",
				"offset": strconv.Itoa(offset),
				"fields": "items(added_at,added_by(id),track(id,name,uri,external_urls,artists(id,name),album(id,name))),next",
			})
		path := fmt.Sprintf("/v1/playlists/%s/tracks", url.PathEscape(playlistID))
		resp, err := c.execute(ctx, req, http.MethodGet, path)
		if err != nil {
			return nil, err
		}
		var page playlistTracksPage
		if err := json.Unmarshal(resp.Body(), &page); err != nil {
			return nil, err
		}
		if len(page.Items) == 0 {
			break
		}
		all = append(all, page.Items...)
		if page.Next == "" {
			break
		}
		offset += len(page.Items)
	}
	return all, nil
}

func (c *spotifyClient) listCategories(ctx context.Context) ([]category, error) {
	categories := make([]category, 0, 64)
	offset := 0
	for {
		req := c.rest.R().
			SetContext(ctx).
			SetQueryParams(map[string]string{
				"limit":  "50",
				"offset": strconv.Itoa(offset),
			})
		resp, err := c.execute(ctx, req, http.MethodGet, "/v1/browse/categories")
		if err != nil {
			return nil, err
		}
		var payload categoryList
		if err := json.Unmarshal(resp.Body(), &payload); err != nil {
			return nil, err
		}
		if len(payload.Categories.Items) == 0 {
			break
		}
		categories = append(categories, payload.Categories.Items...)
		if payload.Categories.Next == "" {
			break
		}
		offset += len(payload.Categories.Items)
	}
	return categories, nil
}

func (c *spotifyClient) categoryPlaylists(ctx context.Context, categoryID string, offset int) (playlistPage, error) {
	req := c.rest.R().
		SetContext(ctx).
		SetQueryParams(map[string]string{
			"limit":  "50",
			"offset": strconv.Itoa(offset),
		})
	path := fmt.Sprintf("/v1/browse/categories/%s/playlists", url.PathEscape(categoryID))
	resp, err := c.execute(ctx, req, http.MethodGet, path)
	if err != nil {
		return playlistPage{}, err
	}
	var payload playlistPageResponse
	if err := json.Unmarshal(resp.Body(), &payload); err != nil {
		return playlistPage{}, err
	}
	return payload.Playlists, nil
}

func (c *spotifyClient) featuredPlaylists(ctx context.Context, offset int) (playlistPage, error) {
	req := c.rest.R().
		SetContext(ctx).
		SetQueryParams(map[string]string{
			"limit":  "50",
			"offset": strconv.Itoa(offset),
		})
	resp, err := c.execute(ctx, req, http.MethodGet, "/v1/browse/featured-playlists")
	if err != nil {
		return playlistPage{}, err
	}
	var payload featuredResponse
	if err := json.Unmarshal(resp.Body(), &payload); err != nil {
		return playlistPage{}, err
	}
	return payload.Playlists, nil
}

func (c *spotifyClient) execute(ctx context.Context, req *resty.Request, method, path string) (*resty.Response, error) {
	for {
		if err := c.limiter.Wait(ctx); err != nil {
			return nil, err
		}
		var resp *resty.Response
		var err error
		switch method {
		case http.MethodGet:
			resp, err = req.Get(path)
		default:
			return nil, fmt.Errorf("unsupported method %s", method)
		}
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, err
			}
			return nil, err
		}
		if resp.StatusCode() == http.StatusTooManyRequests {
			wait := parseRetryAfter(resp)
			if wait <= 0 {
				wait = 2 * time.Second
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(wait):
			}
			continue
		}
		if resp.IsError() {
			body := strings.TrimSpace(string(resp.Body()))
			if len(body) > 512 {
				body = body[:512] + "..."
			}
			return nil, fmt.Errorf("spotify %s %s failed with status %d: %s", method, path, resp.StatusCode(), body)
		}
		return resp, nil
	}
}

func parseRetryAfter(resp *resty.Response) time.Duration {
	if resp == nil {
		return 0
	}
	if v := resp.Header().Get("Retry-After"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil {
			return time.Duration(secs) * time.Second
		}
		if t, err := time.Parse(time.RFC1123, v); err == nil {
			d := time.Until(t)
			if d < 0 {
				return 0
			}
			return d
		}
	}
	if v := resp.Header().Get("Retry-After-Ms"); v != "" {
		if ms, err := strconv.Atoi(v); err == nil {
			return time.Duration(ms) * time.Millisecond
		}
	}
	return 0
}

func newRateLimiter(rate float64) *rateLimiter {
	interval := time.Duration(float64(time.Second) / rate)
	if interval <= 0 {
		interval = 100 * time.Millisecond
	}
	return &rateLimiter{ticker: time.NewTicker(interval), interval: interval}
}

func (r *rateLimiter) Wait(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-r.ticker.C:
		return nil
	}
}

func (r *rateLimiter) Stop() {
	if r.ticker != nil {
		r.ticker.Stop()
	}
}

func newCSVStore(path string, header []string) (*csvStore, error) {
	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create csv dir: %w", err)
		}
	}
	_, err := os.Stat(path)
	exists := err == nil
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open csv %s: %w", path, err)
	}
	writer := csv.NewWriter(file)
	if !exists {
		if err := writer.Write(header); err != nil {
			file.Close()
			return nil, fmt.Errorf("write csv header: %w", err)
		}
		writer.Flush()
		if err := writer.Error(); err != nil {
			file.Close()
			return nil, fmt.Errorf("flush csv header: %w", err)
		}
	}
	return &csvStore{file: file, writer: writer}, nil
}

func (c *csvStore) Write(record []string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.writer.Write(record); err != nil {
		return err
	}
	c.writer.Flush()
	return c.writer.Error()
}

func (c *csvStore) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writer.Flush()
	if err := c.writer.Error(); err != nil {
		c.file.Close()
		return err
	}
	return c.file.Close()
}

func loadSnapshotCache(path string) (*snapshotCache, error) {
	cache := &snapshotCache{path: path, data: make(map[string]string)}
	if path == "" {
		return cache, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cache, nil
		}
		return nil, fmt.Errorf("read snapshot cache: %w", err)
	}
	if len(data) == 0 {
		return cache, nil
	}
	if err := json.Unmarshal(data, &cache.data); err != nil {
		return nil, fmt.Errorf("parse snapshot cache: %w", err)
	}
	return cache, nil
}

func (s *snapshotCache) IsUnchanged(id, snapshot string) bool {
	if snapshot == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	prev, ok := s.data[id]
	return ok && prev == snapshot
}

func (s *snapshotCache) Update(id, snapshot string) {
	if s == nil || snapshot == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.data[id]; ok && existing == snapshot {
		return
	}
	s.data[id] = snapshot
	s.dirty = true
}

func (s *snapshotCache) Save() error {
	if s == nil || s.path == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.dirty {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("ensure snapshot dir: %w", err)
	}
	data, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal snapshot cache: %w", err)
	}
	if err := os.WriteFile(s.path, data, 0o644); err != nil {
		return fmt.Errorf("write snapshot cache: %w", err)
	}
	s.dirty = false
	return nil
}

func newHarvester(client *spotifyClient, seeds *harvestSeeds, playlists, tracks *csvStore, snapshots *snapshotCache, opts harvestOptions) *harvester {
	index := buildSeedIndex(seeds)
	return &harvester{
		client:    client,
		seeds:     seeds,
		index:     index,
		playlists: playlists,
		tracks:    tracks,
		snapshots: snapshots,
		seen:      make(map[string]struct{}),
		opts:      opts,
	}
}
func (h *harvester) harvestSearch(ctx context.Context, queries []seedQuery) error {
	var errs []string
	pages := h.opts.MaxSearchPages
	if pages <= 0 {
		pages = 1
	}
	for _, query := range queries {
		for page := 0; page < pages; page++ {
			offset := page * 50
			pageData, err := h.client.searchPlaylists(ctx, query.Query, offset)
			if err != nil {
				errs = append(errs, fmt.Sprintf("query %q offset %d: %v", query.Query, offset, err))
				break
			}
			if len(pageData.Items) == 0 {
				break
			}
			for _, pl := range pageData.Items {
				if pl.ID == "" {
					continue
				}
				origin := harvestOrigin{Source: fmt.Sprintf("search:%s", query.Source), Query: query.Query}
				if err := h.processPlaylist(ctx, pl.ID, origin); err != nil {
					errs = append(errs, fmt.Sprintf("playlist %s: %v", pl.ID, err))
				}
			}
			if pageData.Next == "" {
				break
			}
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf(strings.Join(errs, "; "))
	}
	return nil
}

func (h *harvester) harvestFeatured(ctx context.Context) error {
	var errs []string
	pages := h.opts.MaxBrowsePages
	if pages <= 0 {
		pages = 1
	}
	for page := 0; page < pages; page++ {
		offset := page * 50
		pageData, err := h.client.featuredPlaylists(ctx, offset)
		if err != nil {
			errs = append(errs, fmt.Sprintf("featured offset %d: %v", offset, err))
			break
		}
		if len(pageData.Items) == 0 {
			break
		}
		for _, pl := range pageData.Items {
			origin := harvestOrigin{Source: "featured", Query: "featured"}
			if err := h.processPlaylist(ctx, pl.ID, origin); err != nil {
				errs = append(errs, fmt.Sprintf("playlist %s: %v", pl.ID, err))
			}
		}
		if pageData.Next == "" {
			break
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf(strings.Join(errs, "; "))
	}
	return nil
}

func (h *harvester) harvestCategories(ctx context.Context) error {
	categories, err := h.client.listCategories(ctx)
	if err != nil {
		return err
	}
	var errs []string
	pages := h.opts.MaxBrowsePages
	if pages <= 0 {
		pages = 1
	}
	for _, cat := range categories {
		for page := 0; page < pages; page++ {
			offset := page * 50
			pageData, err := h.client.categoryPlaylists(ctx, cat.ID, offset)
			if err != nil {
				errs = append(errs, fmt.Sprintf("category %s offset %d: %v", cat.ID, offset, err))
				break
			}
			if len(pageData.Items) == 0 {
				break
			}
			for _, pl := range pageData.Items {
				origin := harvestOrigin{Source: fmt.Sprintf("category:%s", cat.ID), Query: cat.Name}
				if err := h.processPlaylist(ctx, pl.ID, origin); err != nil {
					errs = append(errs, fmt.Sprintf("playlist %s: %v", pl.ID, err))
				}
			}
			if pageData.Next == "" {
				break
			}
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf(strings.Join(errs, "; "))
	}
	return nil
}

func (h *harvester) processPlaylist(ctx context.Context, playlistID string, origin harvestOrigin) error {
	if playlistID == "" {
		return nil
	}
	if h.markSeen(playlistID) {
		return nil
	}
	detail, err := h.client.getPlaylist(ctx, playlistID)
	if err != nil {
		return fmt.Errorf("get playlist: %w", err)
	}
	if h.snapshots != nil && h.snapshots.IsUnchanged(playlistID, detail.SnapshotID) {
		return nil
	}
	tracks, err := h.client.getPlaylistTracks(ctx, playlistID)
	if err != nil {
		return fmt.Errorf("get tracks: %w", err)
	}
	relevance := computeRelevance(detail, tracks, h.index)
	h.snapshots.Update(playlistID, detail.SnapshotID)
	if relevance.Score < h.opts.ScoreThreshold {
		log.Printf("skip playlist %s (%s) with score %.2f", detail.Name, playlistID, relevance.Score)
		return nil
	}
	if err := h.persistPlaylist(detail, origin, relevance); err != nil {
		return err
	}
	if err := h.persistTracks(detail.ID, tracks, origin); err != nil {
		return err
	}
	return nil
}

func (h *harvester) persistPlaylist(detail *playlistDetail, origin harvestOrigin, relevance relevanceResult) error {
	if detail == nil {
		return nil
	}
	record := []string{
		detail.ID,
		detail.Name,
		sanitizeCSVField(detail.Description),
		strconv.Itoa(detail.Followers.Total),
		strconv.FormatBool(detail.Public),
		strconv.FormatBool(detail.Collaborative),
		detail.Owner.ID,
		detail.Owner.DisplayName,
		origin.Source,
		origin.Query,
		fmt.Sprintf("%.2f", relevance.Score),
		strings.Join(relevance.KeywordMatches, "|"),
		strings.Join(relevance.ArtistMatches, "|"),
		strings.Join(relevance.TrackMatches, "|"),
		detail.SnapshotID,
		selectImageURL(detail.Images),
		strconv.Itoa(detail.Tracks.Total),
		strconv.Itoa(relevance.FreshnessDays),
		time.Now().UTC().Format(time.RFC3339),
	}
	return h.playlists.Write(record)
}

func (h *harvester) persistTracks(playlistID string, items []playlistTrackItem, origin harvestOrigin) error {
	for _, item := range items {
		if item.Track.ID == "" {
			continue
		}
		artists := make([]string, 0, len(item.Track.Artists))
		for _, a := range item.Track.Artists {
			artists = append(artists, a.Name)
		}
		record := []string{
			playlistID,
			item.Track.ID,
			item.Track.Name,
			strings.Join(artists, "|"),
			item.Track.Album.ID,
			item.AddedAt,
			item.AddedBy.ID,
			item.Track.ExternalUrls["spotify"],
			origin.Source,
			origin.Query,
		}
		if err := h.tracks.Write(record); err != nil {
			return err
		}
	}
	return nil
}

func (h *harvester) markSeen(id string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.seen[id]; ok {
		return true
	}
	h.seen[id] = struct{}{}
	return false
}

func buildSeedIndex(seeds *harvestSeeds) *seedIndex {
	if seeds == nil {
		seeds = defaultHarvestSeeds()
	}
	idx := &seedIndex{
		keywords:     make([]string, 0, len(seeds.Keywords)+len(seeds.Genres)+len(seeds.Moods)+len(seeds.Meta)+len(seeds.Locales)+len(seeds.Tracks)),
		artistByName: make(map[string]seedArtist),
		artistByID:   make(map[string]seedArtist),
		trackByID:    make(map[string]seedTrack),
	}
	addKeyword := func(value string) {
		v := strings.TrimSpace(strings.ToLower(value))
		if v == "" {
			return
		}
		idx.keywords = append(idx.keywords, v)
	}
	lists := [][]string{seeds.Keywords, seeds.Genres, seeds.Moods, seeds.Meta, seeds.Locales}
	for _, list := range lists {
		for _, item := range list {
			addKeyword(item)
		}
	}
	for _, track := range seeds.Tracks {
		if track.ID != "" {
			idx.trackByID[track.ID] = track
		}
		addKeyword(track.Name)
	}
	for _, artist := range seeds.Artists {
		if artist.SpotifyID != "" {
			idx.artistByID[strings.ToLower(artist.SpotifyID)] = artist
		}
		addKeyword(artist.Name)
		idx.artistByName[strings.ToLower(artist.Name)] = artist
		for _, alias := range artist.Aliases {
			idx.artistByName[strings.ToLower(alias)] = artist
			addKeyword(alias)
		}
	}
	return idx
}

func computeRelevance(detail *playlistDetail, tracks []playlistTrackItem, idx *seedIndex) relevanceResult {
	if idx == nil {
		idx = buildSeedIndex(defaultHarvestSeeds())
	}
	var result relevanceResult
	if detail == nil {
		return result
	}
	text := strings.ToLower(detail.Name + " " + detail.Description)
	keywordMatches := make(map[string]struct{})
	for _, keyword := range idx.keywords {
		if keyword == "" {
			continue
		}
		if strings.Contains(text, keyword) {
			keywordMatches[keyword] = struct{}{}
		}
	}

	artistMatches := make(map[string]struct{})
	trackMatches := make(map[string]struct{})
	var latest time.Time

	for _, item := range tracks {
		if item.AddedAt != "" {
			if t, err := time.Parse(time.RFC3339, item.AddedAt); err == nil {
				if t.After(latest) {
					latest = t
				}
			}
		}
		trackID := item.Track.ID
		if trackID != "" {
			if track, ok := idx.trackByID[trackID]; ok {
				trackMatches[track.Name] = struct{}{}
			}
		}
		for _, artist := range item.Track.Artists {
			if artist.ID != "" {
				if seedArtist, ok := idx.artistByID[strings.ToLower(artist.ID)]; ok {
					artistMatches[seedArtist.Name] = struct{}{}
				}
			}
			if seedArtist, ok := idx.artistByName[strings.ToLower(artist.Name)]; ok {
				artistMatches[seedArtist.Name] = struct{}{}
			}
		}
	}

	keywordList := setToSortedSlice(keywordMatches)
	artistList := setToSortedSlice(artistMatches)
	trackList := setToSortedSlice(trackMatches)

	followerBoost := math.Log10(float64(detail.Followers.Total) + 1)
	freshnessBoost := 0.0
	freshnessDays := -1
	if !latest.IsZero() {
		days := time.Since(latest).Hours() / 24
		if days < 0 {
			days = 0
		}
		freshnessDays = int(math.Round(days))
		if days < 30 {
			freshnessBoost = (30 - days) / 30 * 2.0
		}
	}

	score := float64(len(keywordList)) * 1.0
	score += float64(len(artistList)) * 1.5
	score += float64(len(trackList)) * 2.0
	score += followerBoost * 0.5
	score += freshnessBoost

	result.Score = score
	result.KeywordMatches = keywordList
	result.ArtistMatches = artistList
	result.TrackMatches = trackList
	result.FollowerBoost = followerBoost
	result.FreshnessBoost = freshnessBoost
	result.FreshnessDays = freshnessDays
	return result
}

func setToSortedSlice(set map[string]struct{}) []string {
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for v := range set {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func sanitizeCSVField(s string) string {
	replaced := strings.ReplaceAll(s, "\r\n", " ")
	replaced = strings.ReplaceAll(replaced, "\n", " ")
	replaced = strings.ReplaceAll(replaced, "\r", " ")
	return strings.TrimSpace(replaced)
}

func selectImageURL(images []struct {
	URL    string `json:"url"`
	Height int    `json:"height"`
	Width  int    `json:"width"`
}) string {
	if len(images) == 0 {
		return ""
	}
	return images[0].URL
}

type clientCredentialsResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

func requestClientCredentialsToken(clientID, clientSecret string) (string, error) {
	client := resty.New()
	resp, err := client.R().
		SetBasicAuth(clientID, clientSecret).
		SetHeader("Content-Type", "application/x-www-form-urlencoded").
		SetBody("grant_type=client_credentials").
		Post("https://accounts.spotify.com/api/token")
	if err != nil {
		return "", err
	}
	if resp.IsError() {
		return "", fmt.Errorf("token request failed with status %d", resp.StatusCode())
	}
	var payload clientCredentialsResponse
	if err := json.Unmarshal(resp.Body(), &payload); err != nil {
		return "", err
	}
	return payload.AccessToken, nil
}
