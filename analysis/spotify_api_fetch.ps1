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
$searchQuery = "this is a variable"
$searchResponse = Invoke-RestMethod -Uri "https://api.spotify.com/v1/search?q=$searchQuery&type=playlist&limit=50" `
  -Method Get `
  -Headers @{ "Authorization" = "Bearer $token" }

$searchResponse

# Oder spezifische Daten aus dem Antwortobjekt extrahieren
$searchResponse.playlists.items | ForEach-Object {
  Write-Host "Playlist Name: $($_.name)"
  Write-Host "Playlist id: $($_.id)"

  $tracksUrl = $_.tracks.href
  # now need to fetch the songs from this tracks url
  # can use preview_url could be also used later down the line in the project
  $tracksResponse = Invoke-RestMethod -Uri $tracksUrl `
        -Method Get `
        -Headers @{ "Authorization" = "Bearer $token" }

    # Gib einige Infos zu jedem Track aus
    $tracksResponse.items | ForEach-Object {
        $track = $_.track
        Write-Host " - Track: $($track.name)"
        Write-Host "   Spotify URL: $($track.external_urls.spotify)"
        Write-Host "   Spotify id: $($track.id)"
        Write-Host "   Release Date: $($track.release_date)"
        Write-Host "   Artist(s): $($track.artists.name -join ", ")"
    }
}

# fetching songs works out, there is a cap with the resp rate here

# info id get from the api
# items.id -
# items.name -
# tracks.href - 
# new api call
# for track in tracks
  # track.id - 
  # track.name -
  # track.external_urls.spotify -
  # track.release_date -
  # artist.name - joined
  # - for artist in artists
    # artist.id
    # artist.name
    # artist.href