package whitelist

import (
	"strings"
	"sync"
	"time"

	"github.com/mirror-media/yt-relay/cms"
	"github.com/mirror-media/yt-relay/config"
	log "github.com/sirupsen/logrus"
)

const refreshCooldown = 1 * time.Minute

// YouTubeAPI implements the Whitelist interface
type YouTubeAPI struct {
	Whitelist config.Whitelists
	CmsURL    string
	mu        sync.RWMutex
	lastFetch time.Time
}

func (api *YouTubeAPI) ValidateChannelID(channelID string) bool {
	if effective, present := api.Whitelist.ChannelIDs[channelID]; present {
		return effective
	}
	// Fallback: viper lowercases map keys when loading from YAML config files
	effective, present := api.Whitelist.ChannelIDs[strings.ToLower(channelID)]
	return present && effective
}

func (api *YouTubeAPI) ValidatePlaylistIDs(playlistID string) bool {
	api.mu.RLock()
	effective, present := api.Whitelist.PlaylistIDs[playlistID]
	api.mu.RUnlock()

	if present && effective {
		return true
	}

	return api.refreshAndValidatePlaylist(playlistID)
}

func (api *YouTubeAPI) refreshAndValidatePlaylist(playlistID string) bool {
	api.mu.Lock()
	defer api.mu.Unlock()

	effective, present := api.Whitelist.PlaylistIDs[playlistID]
	if present && effective {
		return true
	}

	if time.Since(api.lastFetch) < refreshCooldown {
		return false
	}

	newIDs, err := cms.FetchPlaylistIDs(api.CmsURL)
	if err != nil {
		log.Errorf("failed to refresh playlist whitelist from CMS: %v", err)
		api.lastFetch = time.Now()
		return false
	}

	api.Whitelist.PlaylistIDs = newIDs
	api.lastFetch = time.Now()

	effective, present = api.Whitelist.PlaylistIDs[playlistID]
	return present && effective
}
