// this go script will use the api in a different way
// we want to retrieve playlists that contain a specific song
// as many as we can
// this data will be handled with also a check for duplicates and stuff
// the database is supposed to grow with the users that use it


// if this is made possible we also should be able to do retrieval with many many more 
// layers of depth

# Spotify Playlist Harvester — Implementation Plan

> Goal: Collect as many *relevant* Spotify playlists as possible (public + user-authorized) using only the official Spotify Web API.

---

## 0) Relevance Definition
- **Seeds:** track IDs, artist IDs, genres, moods/activities, keywords, locales.
- A playlist is *relevant* if:
  - Title/description matches seed keywords.
  - Contains seed track(s) or artist(s).
  - Textual similarity to seeds is above a threshold (optional embeddings).
- **Priorities:** follower count, last-change recency, number of seed matches, editorial category.

---

## 1) API Endpoints Used
- **Auth:** Authorization Code flow  
  Scopes: `playlist-read-private`, `playlist-read-collaborative`.
- **Search playlists:**  
  `GET /v1/search?q=<query>&type=playlist&limit=50&offset=<n>`
- **Browse (editorial):**  
  `GET /v1/browse/categories`  
  `GET /v1/browse/categories/{id}/playlists`  
  `GET /v1/browse/featured-playlists`
- **User playlists (opt-in):**  
  `GET /v1/me/playlists`  
  `GET /v1/users/{user_id}/playlists`
- **Playlist details & tracks:**  
  `GET /v1/playlists/{playlist_id}?fields=...`  
  `GET /v1/playlists/{playlist_id}/tracks?limit=100&offset=<n>`
- **Optional enrichment:**  
  `GET /v1/artists/{id}/related-artists`  
  `GET /v1/tracks` / `GET /v1/audio-features`

---

## 2) Query Strategy
- **Keyword buckets:**
  - Genres: `rock`, `indie pop`, `techno`, `k-pop`, etc.
  - Moods/activities: `study`, `focus`, `workout`, `party`.
  - Meta: `best`, `top`, `throwback`, `essentials`.
  - Locales: `deutsch`, `español`, `français`, `日本語`.
  - Artist/track seeds: canonical + aliases.
- **Combinations:** `<mood> <genre>`, `<artist> best`, `<locale> <genre>`.
- **Pagination:** loop over `offset` in steps of 50 until no more results.
- **Editorial pass:** crawl categories + featured playlists.

---

## 3) Crawl Pipeline
1. **Seed queue:** generated queries + categories.
2. **Search stage:** call `/search` → collect playlist IDs & metadata.
3. **Enrich stage:**  
   - Fetch `/playlists/{id}` (with `fields` to trim).  
   - Fetch `/playlists/{id}/tracks` (100/page).  
   - Store `snapshot_id`; skip if unchanged.
4. **Relevance scoring:** apply keyword/artist/track match rules.
5. **Persist:** playlists, tracks, scores in DB.
6. **Scheduler:** rotate seeds; back off on 429; checkpoint offsets.

---

## 4) Rate Limiting & Robustness
- Global rate limiter: ~5–10 req/s max.
- Handle `429 Too Many Requests`: read `Retry-After`, exponential backoff + jitter.
- Use `ETag` and `If-None-Match` to avoid re-fetching unchanged data.
- Use `fields` param to shrink responses.
- Persist checkpoints for resume.

---

## 5) Relevance Scoring
- **Signals:**
  - Keyword matches in title/description.
  - # of seed artists in tracks.
  - # of seed tracks in playlist.
  - Playlist follower count (log-scaled).
  - Freshness via recent `snapshot_id` change.
- Combine as weighted sum → threshold → mark relevant.

---

## 6) Storage Schema
**playlists**  
- `playlist_id` (PK), `name`, `description`, `owner_id`, `followers`, `public`, `collaborative`, `snapshot_id`, `image_url`, `last_seen_at`

**playlist_tracks**  
- (`playlist_id`, `track_id`, `position`, `added_at`, `added_by`)

**tracks**  
- `track_id` (PK), `name`, `album_id`, `artist_ids[]`

**scores**  
- `playlist_id` (PK), `relevance_score`, `matched_keywords[]`, `matched_artists[]`, `matched_tracks[]`

Indexes:  
- `playlist_tracks(track_id)` for fast “contains” queries.  
- Full-text index on `playlists(name, description)`.

---

## 7) Incremental Sync
- Compare stored `snapshot_id` with latest from `/playlists/{id}`.  
- If unchanged → skip track fetch.  
- If changed → fetch pages, diff against stored tracks.

---

## 8) Quality Control
- Deduplicate by `playlist_id`.  
- Normalize text (lowercase, strip emojis).  
- Filter spam/low-quality: zero followers, duplicates, near-identical track sets.

---

## 9) Ops & Scaling
- **MVP:** 200–500 queries, SQLite/Postgres, single worker.
- **Scale:** worker pool with global limiter, distributed queue (Redis/SQS).  
- **Monitoring:** requests/min, error rate, 429 frequency, crawl coverage.

---

## 10) Pseudocode Outline
```pseudo
seed_queries = generate_queries(seeds)
seen = Set()

for q in seed_queries:
  for offset in range(0, ... step=50):
    results = search_playlists(q, offset=offset)
    if empty(results): break

    for playlist in results:
      if playlist.id in seen: continue
      seen.add(playlist.id)

      meta = get_playlist(playlist.id, fields=...)
      if snapshot_id unchanged: continue

      tracks = []
      for toffset in range(0, ... step=100):
        page = get_playlist_tracks(playlist.id, offset=toffset)
        tracks += page.items
        if page.done: break

      score = relevance(meta, tracks, seeds)
      persist(meta, tracks, score)
      cache.snapshot(playlist.id, meta.snapshot_id)
