package relay

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	ytrelay "github.com/mirror-media/yt-relay"
	"github.com/mirror-media/yt-relay/api"
	log "github.com/sirupsen/logrus"
	"google.golang.org/api/option"
	"google.golang.org/api/youtube/v3"
)

// YouTubeServiceV3 implements the VideoRelay interface and provides api for searching videos with youtube sdk v3
type YouTubeServiceV3 struct {
	youtubeService *youtube.Service
}

func New(apiKey string) (*YouTubeServiceV3, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("apikey is empty for youtube service")
	}
	s, err := youtube.NewService(context.Background(), option.WithAPIKey(apiKey))
	return &YouTubeServiceV3{
		youtubeService: s,
	}, err
}

// Search supports the following parameters: part, channelId, eventType, q, maxResults, pageToken, order, safeSearch, type
func (s *YouTubeServiceV3) Search(options ytrelay.Options) (resp interface{}, err error) {
	yt := s.youtubeService
	call := yt.Search.List(strings.Split(options.Part, ","))
	if !isZero(options.ChannelID) {
		call.ChannelId(options.ChannelID)
	}
	if !isZero(options.EventType) {
		call.EventType(options.EventType)
	}
	if !isZero(options.Query) {
		call.Q(options.Query)
	}
	if !isZero(options.MaxResults) {
		call.MaxResults(options.MaxResults)
	}
	if !isZero(options.PageToken) {
		call.PageToken(options.PageToken)
	}
	if !isZero(options.Order) {
		call.Order(options.Order)
	}
	if !isZero(options.SafeSearch) {
		call.SafeSearch(options.SafeSearch)
	}
	if !isZero(options.Type) {
		call.Type(options.Type)
	}

	return call.Do()
}

// ListByVideoIDs supports the following parameters: part, id, maxResults, pageToken
func (s *YouTubeServiceV3) ListByVideoIDs(options ytrelay.Options) (resp interface{}, err error) {
	yt := s.youtubeService
	call := yt.Videos.List(strings.Split(options.Part, ","))
	if !isZero(options.IDs) {
		call.Id(strings.Split(options.IDs, ",")...)
	} else {
		return nil, fmt.Errorf("parameter \"id\" is mandantory")
	}
	if !isZero(options.PageToken) {
		call.PageToken(options.PageToken)
	}
	if !isZero(options.MaxResults) {
		call.MaxResults(options.MaxResults)
	}
	return call.Do()
}

// ListPlaylistVideos supports the following parameters: part, playlistId, maxResults, pageToken
func (s *YouTubeServiceV3) ListPlaylistVideos(options ytrelay.Options) (resp interface{}, err error) {
	yt := s.youtubeService
	call := yt.PlaylistItems.List(strings.Split(options.Part, ","))
	if !isZero(options.Fields) {
		call.PlaylistId(options.Fields)
	}
	if !isZero(options.PlaylistID) {
		call.PlaylistId(options.PlaylistID)
	}
	if !isZero(options.PageToken) {
		call.PageToken(options.PageToken)
	}
	if !isZero(options.MaxResults) {
		call.MaxResults(options.MaxResults)
	}
	return call.Do()
}

// ListPlaylistVideosAfter fetches playlist items and stops when it encounters videos published before the given time.
// Returns a flat list of filtered videos instead of raw YouTube pagination.
func (s *YouTubeServiceV3) ListPlaylistVideosAfter(options ytrelay.Options) (resp interface{}, err error) {
	yt := s.youtubeService

	if isZero(options.PlaylistID) {
		return nil, fmt.Errorf("parameter \"playlistId\" is mandatory")
	}
	if isZero(options.PublishedAfter) {
		return nil, fmt.Errorf("parameter \"publishedAfter\" is mandatory")
	}

	threshold, err := time.Parse(time.RFC3339, options.PublishedAfter)
	if err != nil {
		return nil, fmt.Errorf("invalid publishedAfter format, expected RFC 3339 (e.g. 2024-01-01T00:00:00Z): %v", err)
	}
	threshold = threshold.UTC()

	parts := strings.Split(options.Part, ",")
	var items []*api.PlaylistItemVideo
	pageToken := ""

	for {
		call := yt.PlaylistItems.List(parts)
		call.PlaylistId(options.PlaylistID)
		call.MaxResults(50)
		if pageToken != "" {
			call.PageToken(pageToken)
		}

		result, err := call.Do()
		if err != nil {
			return nil, fmt.Errorf("failed to list playlist items: %v", err)
		}

		reachedOldVideos := false
		for _, item := range result.Items {
			videoPublishedAt := item.ContentDetails.VideoPublishedAt
			if videoPublishedAt == "" {
				continue
			}

			t, err := time.Parse(time.RFC3339, videoPublishedAt)
			if err != nil {
				log.Warnf("failed to parse videoPublishedAt for video %s: %v", item.ContentDetails.VideoId, err)
				continue
			}

			if t.UTC().Before(threshold) {
				reachedOldVideos = true
				break
			}

			videoItem := &api.PlaylistItemVideo{
				VideoID:          item.ContentDetails.VideoId,
				Title:            item.Snippet.Title,
				Description:      item.Snippet.Description,
				VideoPublishedAt: videoPublishedAt,
				PublishedAt:      item.Snippet.PublishedAt,
				Thumbnails:       convertThumbnails(item.Snippet.Thumbnails),
				VideoURL:         fmt.Sprintf("https://www.youtube.com/watch?v=%s", item.ContentDetails.VideoId),
			}
			items = append(items, videoItem)
		}

		if reachedOldVideos || result.NextPageToken == "" {
			break
		}
		pageToken = result.NextPageToken
	}

	if items == nil {
		items = []*api.PlaylistItemVideo{}
	}

	return &api.PlaylistItemListResponse{
		PlaylistID: options.PlaylistID,
		Items:      items,
		TotalItems: len(items),
	}, nil
}

// ListPlaylists fetches all playlists for a channel, filters by title keywords (q parameter, comma-separated),
// fetches the latest update time for each matching playlist, and optionally filters by publishedAfter.
func (s *YouTubeServiceV3) ListPlaylists(options ytrelay.Options) (resp interface{}, err error) {
	yt := s.youtubeService

	if isZero(options.ChannelID) {
		return nil, fmt.Errorf("parameter \"channelId\" is mandatory")
	}

	// Parse publishedAfter threshold if provided (must be RFC 3339 / UTC)
	var publishedAfterThreshold *time.Time
	if !isZero(options.PublishedAfter) {
		t, err := time.Parse(time.RFC3339, options.PublishedAfter)
		if err != nil {
			return nil, fmt.Errorf("invalid publishedAfter format, expected RFC 3339 (e.g. 2024-01-01T00:00:00Z): %v", err)
		}
		utc := t.UTC()
		publishedAfterThreshold = &utc
	}

	// Parse title keywords from q parameter (comma-separated)
	var keywords []string
	if !isZero(options.Query) {
		for _, kw := range strings.Split(options.Query, ",") {
			kw = strings.TrimSpace(kw)
			if kw != "" {
				keywords = append(keywords, kw)
			}
		}
	}

	// Fetch all playlists for the channel (paginate through all pages)
	var allPlaylists []*youtube.Playlist
	pageToken := ""
	for {
		call := yt.Playlists.List([]string{"snippet", "contentDetails"})
		call.ChannelId(options.ChannelID)
		call.MaxResults(50)
		if pageToken != "" {
			call.PageToken(pageToken)
		}

		result, err := call.Do()
		if err != nil {
			return nil, fmt.Errorf("failed to list playlists: %v", err)
		}

		allPlaylists = append(allPlaylists, result.Items...)

		if result.NextPageToken == "" {
			break
		}
		pageToken = result.NextPageToken
	}

	// Filter by title keywords (if provided)
	var filtered []*youtube.Playlist
	if len(keywords) > 0 {
		for _, pl := range allPlaylists {
			for _, kw := range keywords {
				if strings.Contains(pl.Snippet.Title, kw) {
					filtered = append(filtered, pl)
					break
				}
			}
		}
	} else {
		filtered = allPlaylists
	}

	// For each matching playlist, build the response and fetch the latest item's time
	var playlists []*api.PlaylistInfo
	for _, pl := range filtered {
		info := &api.PlaylistInfo{
			ID:          pl.Id,
			Title:       pl.Snippet.Title,
			Description: pl.Snippet.Description,
			PublishedAt: pl.Snippet.PublishedAt,
			ItemCount:   pl.ContentDetails.ItemCount,
			Thumbnails:  convertThumbnails(pl.Snippet.Thumbnails),
		}

		// Fetch the most recent item to determine the playlist's last update time
		if pl.ContentDetails.ItemCount > 0 {
			itemCall := yt.PlaylistItems.List([]string{"snippet", "contentDetails"})
			itemCall.PlaylistId(pl.Id)
			itemCall.MaxResults(1)

			itemResult, err := itemCall.Do()
			if err != nil {
				log.Warnf("failed to fetch latest item for playlist %s: %v", pl.Id, err)
			} else if len(itemResult.Items) > 0 {
				item := itemResult.Items[0]
				info.LastUpdatedAt = item.Snippet.PublishedAt
			}
		}

		// Apply publishedAfter filter
		if publishedAfterThreshold != nil {
			if info.LastUpdatedAt == "" {
				continue
			}
			lastUpdated, err := time.Parse(time.RFC3339, info.LastUpdatedAt)
			if err != nil {
				log.Warnf("failed to parse lastUpdatedAt for playlist %s: %v", pl.Id, err)
				continue
			}
			if lastUpdated.UTC().Before(*publishedAfterThreshold) {
				continue
			}
		}

		playlists = append(playlists, info)
	}

	if playlists == nil {
		playlists = []*api.PlaylistInfo{}
	}

	return &api.PlaylistListResponse{Playlists: playlists}, nil
}

func convertThumbnails(t *youtube.ThumbnailDetails) *api.PlaylistThumbnails {
	if t == nil {
		return nil
	}
	result := &api.PlaylistThumbnails{}
	if t.Default != nil {
		result.Default = &api.Thumbnail{URL: t.Default.Url, Width: t.Default.Width, Height: t.Default.Height}
	}
	if t.Medium != nil {
		result.Medium = &api.Thumbnail{URL: t.Medium.Url, Width: t.Medium.Width, Height: t.Medium.Height}
	}
	if t.High != nil {
		result.High = &api.Thumbnail{URL: t.High.Url, Width: t.High.Width, Height: t.High.Height}
	}
	if t.Standard != nil {
		result.Standard = &api.Thumbnail{URL: t.Standard.Url, Width: t.Standard.Width, Height: t.Standard.Height}
	}
	if t.Maxres != nil {
		result.Maxres = &api.Thumbnail{URL: t.Maxres.Url, Width: t.Maxres.Width, Height: t.Maxres.Height}
	}
	return result
}

func isZero(i interface{}) bool {
	v := reflect.ValueOf(i)
	return !v.IsValid() || reflect.DeepEqual(v.Interface(), reflect.Zero(v.Type()).Interface())
}
