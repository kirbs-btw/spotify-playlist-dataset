package main

import (
	"encoding/csv"
	"encoding/json"
	"log"
	"os"
	"bufio"
	"strings"
	"strconv"
	"fmt"
	"flag"

	"github.com/go-resty/resty/v2"
	"github.com/joho/godotenv"
)

// structure Spotify-API
type SearchResponse struct {
	Playlists PlaylistPage `json:"playlists"`
}

type PlaylistPage struct {
	Items []Playlist `json:"items"`
}

type Playlist struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Tracks struct {
		Href string `json:"href"`
	} `json:"tracks"`
}

type TracksResponse struct {
	Items []struct {
		Track Track `json:"track"`
	} `json:"items"`
}

type Track struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	ExternalUrls map[string]string `json:"external_urls"`
	ReleaseDate  string            `json:"release_date"`
	Artists      []struct {
		Name string `json:"name"`
	} `json:"artists"`
}

func main() {
	envFile := flag.String("env", ".env", "Path to .env file")
	keyword_idx := flag.String("kw_idx", "44", "idx shift in the keywords file")
	keyword_file := flag.String("kw_file", "keywords_en.txt", "file of the keywords")
	playlist_file_name := flag.String("pl_file_name", "data/playlists.csv", "file to save playlists to")
	song_file_name := flag.String("s_file_name", "data/songs.csv", "file of the save songs to")
    flag.Parse()
	// exp.: go run scripts/main.go --env=.env
	fmt.Println("envF:", envFile)	

	// load .env
	err := godotenv.Load(*envFile)
	if err != nil {
		log.Fatal("Fehler beim Laden der .env Datei: ", err)
	}
	clientID := os.Getenv("CLIENT_ID")
	clientSecret := os.Getenv("CLIENT_SECRET")

	fmt.Println("cID:", clientID)
	fmt.Println("cS:", clientSecret)

	// get Spotify-Token
	token, err := getSpotifyToken(clientID, clientSecret)
	if err != nil {
		// log.Fatalf("Fehler beim Holen des Tokens: %v", err)
	}

	// CSV-Dateien init
	plFile, plWriter := createCSV(*playlist_file_name, []string{"playlist_id", "playlist_name", "tracks_href"})
	defer plFile.Close()

	songFile, songWriter := createCSV(*song_file_name, []string{"playlist_id", "track_id", "track_name", "track_external_urls", "release_date", "artist_name"})
	defer songFile.Close()
	
	// List of common words
	// get keywords
	// Open the file
	file_path := fmt.Sprintf("keywords/%s", *keyword_file)
    file, err := os.Open(file_path)
    if err != nil {
        fmt.Println("Error opening file:", err)
        return
    }
    defer file.Close()

	var keywords []string
	
    scanner := bufio.NewScanner(file)
    for scanner.Scan() {
        keyword := scanner.Text()
		keywords = append(keywords, keyword)
    }

	if err := scanner.Err(); err != nil {
        fmt.Println("Error reading file:", err)
        return
    }

	idx_shift, err := strconv.Atoi(*keyword_idx)
	if err != nil {
		fmt.Println("The index shift was not an integer")
		return
	}
	
	for i, keyword := range keywords[idx_shift:] {
		query := keyword
		fmt.Printf("Current query: %s\n", query)
		fmt.Printf("Idx query: %s\n", i)
		for offset := 0; offset < 21; offset++ {
            offsetStr := strconv.Itoa(offset * 50)
			fmt.Printf("Batch: %s\n", offsetStr)
            fetchAndSave(token, query, plWriter, songWriter, offsetStr)
		}
    }
}

// getSpotifyToken get Client-Credentials-Token
type tokenResponse struct {
	AccessToken string `json:"access_token"`
}

func getSpotifyToken(clientID, clientSecret string) (string, error) {
	client := resty.New()
	resp, err := client.R().
		SetBasicAuth(clientID, clientSecret).
		SetHeader("Content-Type", "application/x-www-form-urlencoded").
		SetBody("grant_type=client_credentials").
		Post("https://accounts.spotify.com/api/token")

	if err != nil {
		return "", err
	}

	var r tokenResponse
	if err := json.Unmarshal(resp.Body(), &r); err != nil {
		return "", err
	}
	return r.AccessToken, nil
}

// createCSV open/create .csv with writer/file
func createCSV(filename string, header []string) (*os.File, *csv.Writer) {
	file, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		log.Fatalf("Fehler beim Öffnen von %s: %v", filename, err)
	}

	info, err := file.Stat()
	if err != nil {
		log.Fatalf("Stat-Fehler bei %s: %v", filename, err)
	}
	writer := csv.NewWriter(file)
	if info.Size() == 0 {
		if err := writer.Write(header); err != nil {
			log.Fatalf("Fehler beim Schreiben des Headers in %s: %v", filename, err)
		}
		writer.Flush()
	}

	return file, writer
}

// fetchAndSave gets playlist and saves in the csv
func fetchAndSave(token, query string, plWriter, songWriter *csv.Writer, offset string) {
	client := resty.New()
	// playlist search
	resp, err := client.R().
		SetAuthToken(token).
		SetQueryParams(map[string]string{"q": query, "type": "playlist", "limit": "50", "offset": offset}).
		Get("https://api.spotify.com/v1/search")
	if err != nil {
		log.Fatalf("Fehler bei Playlist-Suche: %v", err)
	}

	var search SearchResponse
	if err := json.Unmarshal(resp.Body(), &search); err != nil {
		// log.Fatalf("JSON-Unmarshal-Fehler: %v", err)
	}

	// tracks of the playlist loop
	for _, pl := range search.Playlists.Items {
		// get tracks
		trackResp, err := client.R().
			SetAuthToken(token).
			Get(pl.Tracks.Href)
		if err != nil {
			log.Printf("Fehler beim Holen der Tracks für %s: %v", pl.ID, err)
			continue
		}
		
		// only save the playlist
		// when tracks can be fetched
		rec := []string{pl.ID, pl.Name, pl.Tracks.Href}
		if err := plWriter.Write(rec); err != nil {
			log.Printf("Fehler beim Schreiben Playlist %s: %v", pl.ID, err)
		}
		plWriter.Flush()
		
		var tr TracksResponse
		if err := json.Unmarshal(trackResp.Body(), &tr); err != nil {
			log.Printf("JSON-Unmarshal-Fehler Tracks für %s: %v", pl.ID, err)
			continue
		}

		for _, item := range tr.Items {
			track := item.Track
			artists := []string{}
			for _, a := range track.Artists {
				artists = append(artists, a.Name)
			}
			external := track.ExternalUrls["spotify"]

			songRec := []string{
				pl.ID,
				track.ID,
				track.Name,
				external,
				track.ReleaseDate,
				strings.Join(artists, ", "),
			}
			if err := songWriter.Write(songRec); err != nil {
				log.Printf("Fehler beim Schreiben Track %s: %v", track.ID, err)
			}
			songWriter.Flush()
		}
	}
}
