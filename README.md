# New Million Song Dataset

Ref.:
https://developer.spotify.com/documentation/web-api/reference/search
https://developer.spotify.com/documentation/web-api

Notes about the Spotify Api: 
Retriving 5 Mil playlist with new data will be quite difficult
for every search there is a maximum of 1000 playlists
and also a maximum of 50 playlist per request ~100k requests to be made with 5000 search request phrases
also noted that the api often only supports 10k playlists an hour ~500 hours if that fact holds true... 
Maybe creating multiple accounts and deploying the script on different seed/offsets... and maybe even the need to alter IPs to not get blocked...
Offset could mean different things.. offset inside the ONE playlist that got fetched of the ofset of the playlists that got fetched

Possible different approches to do the search
main goal right now is to get data to songs in 3 Languages English, Spanish and German for the purpouse of this mvp

Naive approche for search:
    a
    b
    c
    ...
    aa
    ab
    ... 

Approche to get new songs data: 
Could go on base on the filter new? Find out what the new songs are... because in the end the user wants to find new stuff.. 
Then take the data from new songs and finde playlists from those songs forward

use the market parameter to maybe only get songs in the different languages? need to look deeper But thats i connected to the own user country settings... hard to alter. Maybe setting up 3 accounts for the regions. 
Generally this param could also mean to support index music and so on to be in line with local legslation