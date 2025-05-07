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

	"github.com/go-resty/resty/v2"
	"github.com/joho/godotenv"
)

// Strukturen für Spotify-API-Antworten
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
	// .env laden
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Fehler beim Laden der .env Datei: ", err)
	}
	clientID := os.Getenv("CLIENT_ID")
	clientSecret := os.Getenv("CLIENT_SECRET")

	// Spotify-Token holen
	token, err := getSpotifyToken(clientID, clientSecret)
	if err != nil {
		log.Fatalf("Fehler beim Holen des Tokens: %v", err)
	}

	// CSV-Dateien initialisieren
	plFile, plWriter := createCSV("data/playlists.csv", []string{"playlist_id", "playlist_name", "tracks_href"})
	defer plFile.Close()

	songFile, songWriter := createCSV("data/songs.csv", []string{"playlist_id", "track_id", "track_name", "track_external_urls", "release_date", "artist_name"})
	defer songFile.Close()

	// get keywords
	// Open the file
    file, err := os.Open("scripts/keywords.txt") // change the filename as needed
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

    // for i, keyword := range keywords {
	// 	// do sth with the keyword
    // }

	chars := "xyz"
    for _, c := range chars {
        query := string(c)
		fmt.Printf("Current query: %s\n", query)
        for offset := 0; offset < 21; offset++ {
            offsetStr := strconv.Itoa(offset * 50)
            fetchAndSave(token, query, plWriter, songWriter, offsetStr)
        }
    }
}

// getSpotifyToken ruft das Client-Credentials-Token ab
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

// createCSV öffnet eine Datei, schreibt ggf. Header und gibt Writer zurück
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

// fetchAndSave holt Playlists und Tracks und schreibt sie in CSV
func fetchAndSave(token, query string, plWriter, songWriter *csv.Writer, offset string) {
	client := resty.New()
	// Playlists suchen
	resp, err := client.R().
		SetAuthToken(token).
		SetQueryParams(map[string]string{"q": query, "type": "playlist", "limit": "50", "offset": offset}).
		Get("https://api.spotify.com/v1/search")
	if err != nil {
		log.Fatalf("Fehler bei Playlist-Suche: %v", err)
	}

	var search SearchResponse
	if err := json.Unmarshal(resp.Body(), &search); err != nil {
		log.Fatalf("JSON-Unmarshal-Fehler: %v", err)
	}

	// Playlists und zugehörige Tracks durchlaufen
	for _, pl := range search.Playlists.Items {
		// Tracks holen
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
