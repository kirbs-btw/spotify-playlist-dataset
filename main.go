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
	// Open or create the CSV file in append mode
	file, err := os.OpenFile("output.csv", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Printf("Failed to open file: %v\n", err)
		return
	}
	defer file.Close()

	// Create a new CSV writer
	writer := csv.NewWriter(file)

	// Example usage of WriteToCSV
	if err := WriteToCSV(writer, "Alice", 30, 88.5); err != nil {
		fmt.Printf("Error writing to CSV: %v\n", err)
	}
}


func WriteToCSV(writer *csv.Writer, name string, age int, score float64) error {
	// string conversion
	ageStr := strconv.Itoa(age)
	scoreStr := fmt.Sprintf("%.2f", score)

	record := []string{name, ageStr, scoreStr}

	if err := writer.Write(record); err != nil {
		return fmt.Errorf("error writing record to CSV: %v", err)
	}

	// flushing
	writer.Flush()
	if err := writer.Error(); err != nil {
		return fmt.Errorf("error flushing CSV writer: %v", err)
	}

	return nil
}
