# .env
$env:CLIENT_ID = Get-Content ".env" | Select-String "CLIENT_ID" | ForEach-Object { $_.Line.Split("=")[1].Trim() }
$env:CLIENT_SECRET = Get-Content ".env" | Select-String "CLIENT_SECRET" | ForEach-Object { $_.Line.Split("=")[1].Trim() }

# access token
$tokenResponse = Invoke-RestMethod -Uri "https://accounts.spotify.com/api/token" `
  -Method Post `
  -Headers @{ "Authorization" = "Basic " + [Convert]::ToBase64String([Text.Encoding]::ASCII.GetBytes("$($env:CLIENT_ID):$($env:CLIENT_SECRET)"))} `
  -Body @{ "grant_type" = "client_credentials" }

$token = $tokenResponse.access_token

# Fetch data from API
$searchQuery = "test"
$searchResponse = Invoke-RestMethod -Uri "https://api.spotify.com/v1/search?q=$searchQuery&type=playlist&limit=50" `
  -Method Get `
  -Headers @{ "Authorization" = "Bearer $token" }

$searchResponse

# Oder spezifische Daten aus dem Antwortobjekt extrahieren
$searchResponse.playlists.items | ForEach-Object {
    Write-Host "Playlist Name: $($_.name)"
    Write-Host "Playlist URL: $($_.external_urls.spotify)"
}

# need to fetch the content somehow - already in the resp but not correct parsed by me
