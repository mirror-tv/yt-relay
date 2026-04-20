package api

type ErrorResp struct {
	Error string `json:"error"`
}

type Thumbnail struct {
	URL    string `json:"url"`
	Width  int64  `json:"width"`
	Height int64  `json:"height"`
}

type PlaylistThumbnails struct {
	Default  *Thumbnail `json:"default,omitempty"`
	Medium   *Thumbnail `json:"medium,omitempty"`
	High     *Thumbnail `json:"high,omitempty"`
	Standard *Thumbnail `json:"standard,omitempty"`
	Maxres   *Thumbnail `json:"maxres,omitempty"`
}

type PlaylistInfo struct {
	ID            string              `json:"id"`
	Title         string              `json:"title"`
	Description   string              `json:"description"`
	PublishedAt   string              `json:"publishedAt"`
	LastUpdatedAt string              `json:"lastUpdatedAt"`
	ItemCount     int64               `json:"itemCount"`
	Thumbnails    *PlaylistThumbnails `json:"thumbnails,omitempty"`
}

type PlaylistListResponse struct {
	Playlists []*PlaylistInfo `json:"playlists"`
}

type PlaylistItemVideo struct {
	VideoID          string              `json:"videoId"`
	Title            string              `json:"title"`
	Description      string              `json:"description"`
	VideoPublishedAt string              `json:"videoPublishedAt"`
	PublishedAt      string              `json:"publishedAt"`
	Thumbnails       *PlaylistThumbnails `json:"thumbnails,omitempty"`
	VideoURL         string              `json:"videoUrl"`
}

type PlaylistItemListResponse struct {
	PlaylistID string               `json:"playlistId"`
	Items      []*PlaylistItemVideo `json:"items"`
	TotalItems int                  `json:"totalItems"`
}
