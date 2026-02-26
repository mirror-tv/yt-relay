package cms

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	log "github.com/sirupsen/logrus"
)

var playlistIDRegex = regexp.MustCompile(`[?&]list=([A-Za-z0-9_-]+)`)

type graphQLRequest struct {
	Query string `json:"query"`
}

type showFields struct {
	PlayList01      *string `json:"playList01"`
	PlayList02      *string `json:"playList02"`
	TrailerPlaylist *string `json:"trailerPlaylist"`
}

type showsResponse struct {
	Data struct {
		Shows []showFields `json:"shows"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

const showsQuery = `{
  shows {
    playList01
    playList02
    trailerPlaylist
  }
}`

// FetchPlaylistIDs fetches all shows from the CMS and extracts playlist IDs
// from playList01, playList02, and trailerPlaylist fields.
func FetchPlaylistIDs(cmsURL string) (map[string]bool, error) {
	reqBody, err := json.Marshal(graphQLRequest{Query: showsQuery})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal GraphQL request: %v", err)
	}

	resp, err := http.Post(cmsURL, "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to fetch shows from CMS: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("CMS returned status %d", resp.StatusCode)
	}

	var result showsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode CMS response: %v", err)
	}

	if len(result.Errors) > 0 {
		return nil, fmt.Errorf("CMS GraphQL error: %s", result.Errors[0].Message)
	}

	playlistIDs := make(map[string]bool)
	for _, show := range result.Data.Shows {
		for _, field := range []*string{show.PlayList01, show.PlayList02, show.TrailerPlaylist} {
			if id := extractPlaylistID(field); id != "" {
				playlistIDs[id] = true
			}
		}
	}

	log.Infof("fetched %d playlist IDs from CMS (%d shows)", len(playlistIDs), len(result.Data.Shows))

	return playlistIDs, nil
}

// extractPlaylistID extracts the YouTube playlist ID from a field value.
// The field may contain a full URL like:
//   https://www.youtube.com/playlist?list=PL1jBQxu5Eklci：《宵夜鏡來講》
// or just the URL without a description. Returns empty string if no valid ID found.
func extractPlaylistID(field *string) string {
	if field == nil {
		return ""
	}

	s := strings.TrimSpace(*field)
	if s == "" {
		return ""
	}

	matches := playlistIDRegex.FindStringSubmatch(s)
	if len(matches) < 2 {
		return ""
	}

	return matches[1]
}
