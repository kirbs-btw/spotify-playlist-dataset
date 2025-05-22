# Million Playlist Dataset
## Goal
This repository is constructing a dataset of playlists from various APIs and methods. The goal is to collect an enormous amount of the latest data, featuring up-to-date songs.

## Methods
### 1. Brute Force
This method generates a vast number of query terms by iterating over combinations of letters, similar to an exhaustive search. It is particularly useful for discovering a wide range of playlists regardless of popularity or genre.

- **Approach:**  
  Sequentially generate search terms in the following order:
  - Single letters: `a`, `b`, `c`, ..., `z`
  - Double letters: `aa`, `ab`, `ac`, ..., `zz`
  - Triple letters and beyond (optional for deeper crawling)

- **Goal:**  
  Maximize coverage of potential playlist titles through exhaustive queries.

- **Considerations:**  
  This method can be API rate-limit intensive and may result in many irrelevant or low-quality playlists.

### 2. List of Common Words
This method uses a predefined list of frequently used words or phrases found in playlist titles to issue targeted search queries.

- **Approach:**  
  Curate or extract a list of common playlist keywords (e.g., `party`, `workout`, `chill`, `throwback`) and use them as search terms.

- **Goal:**  
  Improve efficiency and relevance by focusing on high-likelihood search terms.

- **Considerations:**  
  More efficient than brute force, but may miss obscure or uniquely named playlists.

## Usage
```sh
go run scripts/main.go --env=.env --kw_idx=29 --kw_file=keywords_sp.txt
```
```sh
go run scripts/main.go --env=.env --kw_idx=57 --kw_file=keywords_en.txt
```
```sh
go run scripts/main.go --env=.env --kw_idx=15 --kw_file=keywords_ger.txt
```

## Api Analysis
**Spotify Api Analysis:** <br>
[Analysis, Limits and Capabilities](/docs/spotify_playlist_api.md)

## References
**Spotify Api Docs:** <br>
[Search Api](https://developer.spotify.com/documentation/web-api/reference/search) <br>
[General Api Docs](https://developer.spotify.com/documentation/web-api
) <br>
**Notes** <br>
[Work in Progress development Notes for this Repo](/docs/wip_notes.md)


