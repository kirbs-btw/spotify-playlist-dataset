package main

import (
	"fmt"
	"log"
	"os"
	"encoding/json"
	"encoding/csv"
	"github.com/go-resty/resty/v2"
	"github.com/joho/godotenv"
)

func main() {
	// .env laden
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Fehler beim Laden der .env Datei")
	}

	clientID := os.Getenv("CLIENT_ID")
	clientSecret := os.Getenv("CLIENT_SECRET")

	token, err := getSpotifyToken(clientID, clientSecret)
	if err != nil {
		log.Fatalf("Fehler beim Holen des Tokens: %v", err)
	}

	// Jetzt API Call mit dem Token
	searchSpotify(token, "workout")
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

	var result map[string]interface{}
	if err := json.Unmarshal(resp.Body(), &result); err != nil {
		return "", err
	}

	token, ok := result["access_token"].(string)
	if !ok {
		return "", fmt.Errorf("Access Token nicht gefunden im Response")
	}

	return token, nil
}


func searchSpotify(token, query string) {
	client := resty.New()

	resp, err := client.R().
		SetAuthToken(token).
		SetQueryParams(map[string]string{
			"q":    query,
			"type": "playlist",
			"limit": "50",
		}).
		Get("https://api.spotify.com/v1/search")

	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Response Body:")
	fmt.Println(string(resp.Body()))
}

func do_csv_stuff() {
	file, err := os.OpenFile("output.csv", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
        log.Fatalf("Failed to create file: %s", err)
    }
    defer file.Close()

	writer := csv.NewWriter(file)

	defer writer.Flush() // Make sure data is written to the file
}

func csv_writer(row) {
	// some values should get in here
	// taking the open csv object i guess
	
	// writing the values to the csv 
	// Write the row to the CSV file
    err = writer.Write(row)
    if err != nil {
        log.Fatalf("Failed to write row: %s", err)
    }
}
