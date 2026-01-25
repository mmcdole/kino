package jellyfin

// AuthResponse represents the response from Jellyfin's AuthenticateByName endpoint
type AuthResponse struct {
	User        User   `json:"User"`
	AccessToken string `json:"AccessToken"`
	ServerID    string `json:"ServerId"`
}

// User represents a Jellyfin user
type User struct {
	ID                        string `json:"Id"`
	Name                      string `json:"Name"`
	ServerID                  string `json:"ServerId"`
	HasPassword               bool   `json:"HasPassword"`
	HasConfiguredPassword     bool   `json:"HasConfiguredPassword"`
	HasConfiguredEasyPassword bool   `json:"HasConfiguredEasyPassword"`
}

// SystemInfo represents the public system info from Jellyfin
type SystemInfo struct {
	LocalAddress      string `json:"LocalAddress"`
	ServerName        string `json:"ServerName"`
	Version           string `json:"Version"`
	ProductName       string `json:"ProductName"`
	OperatingSystem   string `json:"OperatingSystem"`
	ID                string `json:"Id"`
	StartupWizardComplete bool `json:"StartupWizardCompleted"`
}

// ItemsResponse represents a paginated list of items from Jellyfin
type ItemsResponse struct {
	Items            []Item `json:"Items"`
	TotalRecordCount int    `json:"TotalRecordCount"`
	StartIndex       int    `json:"StartIndex"`
}

// Item represents a media item from Jellyfin (movie, show, season, episode, etc.)
type Item struct {
	ID                   string    `json:"Id"`
	Name                 string    `json:"Name"`
	SortName             string    `json:"SortName"`
	Overview             string    `json:"Overview"`
	Type                 string    `json:"Type"`
	CollectionType       string    `json:"CollectionType,omitempty"` // For libraries: "movies", "tvshows"
	DateCreated          string    `json:"DateCreated,omitempty"`
	DateLastMediaAdded   string    `json:"DateLastMediaAdded,omitempty"` // When last episode was added to show
	PremiereDate         string    `json:"PremiereDate,omitempty"`
	ProductionYear       int       `json:"ProductionYear,omitempty"`
	RunTimeTicks         int64     `json:"RunTimeTicks,omitempty"` // Duration in 100-nanosecond units
	CommunityRating      float64   `json:"CommunityRating,omitempty"`
	OfficialRating       string    `json:"OfficialRating,omitempty"`
	ImageTags            ImageTags `json:"ImageTags,omitempty"`
	BackdropImageTags    []string  `json:"BackdropImageTags,omitempty"`
	ParentID             string    `json:"ParentId,omitempty"`
	SeriesID             string    `json:"SeriesId,omitempty"`
	SeriesName           string    `json:"SeriesName,omitempty"`
	SeasonID             string    `json:"SeasonId,omitempty"`
	SeasonName           string    `json:"SeasonName,omitempty"`
	ParentIndexNumber    int       `json:"ParentIndexNumber,omitempty"` // Season number
	IndexNumber          int       `json:"IndexNumber,omitempty"`       // Episode number
	ChildCount           int       `json:"ChildCount,omitempty"`        // Number of child items (seasons for show, episodes for season)
	RecursiveItemCount   int       `json:"RecursiveItemCount,omitempty"` // Total items recursively (episodes for show)
	UserData             *UserData `json:"UserData,omitempty"`
	MediaSources         []MediaSource `json:"MediaSources,omitempty"`
	Path                 string    `json:"Path,omitempty"`
	Container            string    `json:"Container,omitempty"`
	MediaStreams         []MediaStream `json:"MediaStreams,omitempty"`
}

// ImageTags contains image tag IDs for various image types
type ImageTags struct {
	Primary string `json:"Primary,omitempty"`
	Thumb   string `json:"Thumb,omitempty"`
	Banner  string `json:"Banner,omitempty"`
	Logo    string `json:"Logo,omitempty"`
}

// UserData contains user-specific data for an item (watch status, progress)
type UserData struct {
	PlaybackPositionTicks int64  `json:"PlaybackPositionTicks"` // Progress in 100-nanosecond units
	PlayCount             int    `json:"PlayCount"`
	IsFavorite            bool   `json:"IsFavorite"`
	Played                bool   `json:"Played"`
	Key                   string `json:"Key"`
	UnplayedItemCount     int    `json:"UnplayedItemCount,omitempty"` // For containers like shows/seasons
}

// MediaSource represents a media source (file) for an item
type MediaSource struct {
	ID               string        `json:"Id"`
	Path             string        `json:"Path"`
	Protocol         string        `json:"Protocol"` // "File" or "Http"
	Type             string        `json:"Type"`
	Container        string        `json:"Container"`
	Size             int64         `json:"Size"`
	Name             string        `json:"Name"`
	RunTimeTicks     int64         `json:"RunTimeTicks"`
	SupportsDirectPlay   bool      `json:"SupportsDirectPlay"`
	SupportsDirectStream bool      `json:"SupportsDirectStream"`
	SupportsTranscoding  bool      `json:"SupportsTranscoding"`
	MediaStreams         []MediaStream `json:"MediaStreams,omitempty"`
	DirectStreamURL      string    `json:"DirectStreamUrl,omitempty"`
}

// MediaStream represents a video, audio, or subtitle stream
type MediaStream struct {
	Codec                string  `json:"Codec"`
	CodecTag             string  `json:"CodecTag,omitempty"`
	Language             string  `json:"Language,omitempty"`
	DisplayTitle         string  `json:"DisplayTitle,omitempty"`
	Type                 string  `json:"Type"` // "Video", "Audio", "Subtitle"
	Index                int     `json:"Index"`
	IsDefault            bool    `json:"IsDefault"`
	IsForced             bool    `json:"IsForced"`
	Height               int     `json:"Height,omitempty"`
	Width                int     `json:"Width,omitempty"`
	BitRate              int     `json:"BitRate,omitempty"`
	Channels             int     `json:"Channels,omitempty"`
	SampleRate           int     `json:"SampleRate,omitempty"`
	AspectRatio          string  `json:"AspectRatio,omitempty"`
	VideoRange           string  `json:"VideoRange,omitempty"`
	VideoRangeType       string  `json:"VideoRangeType,omitempty"`
}

// PlaybackInfoResponse contains playback information for an item
type PlaybackInfoResponse struct {
	MediaSources    []MediaSource `json:"MediaSources"`
	PlaySessionID   string        `json:"PlaySessionId"`
}

// SearchHint represents a search result from Jellyfin
type SearchHint struct {
	ID              string `json:"Id"`
	Name            string `json:"Name"`
	Type            string `json:"Type"`
	RunTimeTicks    int64  `json:"RunTimeTicks,omitempty"`
	ProductionYear  int    `json:"ProductionYear,omitempty"`
	ParentIndexNumber int  `json:"ParentIndexNumber,omitempty"`
	IndexNumber     int    `json:"IndexNumber,omitempty"`
	SeriesName      string `json:"Series,omitempty"`
	Album           string `json:"Album,omitempty"`
	PrimaryImageTag string `json:"PrimaryImageTag,omitempty"`
	ThumbImageTag   string `json:"ThumbImageTag,omitempty"`
}

// SearchHintsResponse represents search results from Jellyfin
type SearchHintsResponse struct {
	SearchHints       []SearchHint `json:"SearchHints"`
	TotalRecordCount  int          `json:"TotalRecordCount"`
}
