package jellyfin

// authResponse represents the response from Jellyfin's AuthenticateByName endpoint
type authResponse struct {
	User        user   `json:"User"`
	AccessToken string `json:"AccessToken"`
	ServerID    string `json:"ServerId"`
}

// user represents a Jellyfin user
type user struct {
	ID                        string `json:"Id"`
	Name                      string `json:"Name"`
	ServerID                  string `json:"ServerId"`
	HasPassword               bool   `json:"HasPassword"`
	HasConfiguredPassword     bool   `json:"HasConfiguredPassword"`
	HasConfiguredEasyPassword bool   `json:"HasConfiguredEasyPassword"`
}

// systemInfo represents the public system info from Jellyfin
type systemInfo struct {
	LocalAddress    string `json:"LocalAddress"`
	ServerName      string `json:"ServerName"`
	Version         string `json:"Version"`
	ProductName     string `json:"ProductName"`
	OperatingSystem string `json:"OperatingSystem"`
	ID              string `json:"Id"`
}

// itemsResponse represents a paginated list of items from Jellyfin
type itemsResponse struct {
	Items            []baseItem `json:"Items"`
	TotalRecordCount int        `json:"TotalRecordCount"`
	StartIndex       int        `json:"StartIndex"`
}

// baseItem is Jellyfin's BaseItemDto: a movie, show, season, episode, etc.
type baseItem struct {
	ID                 string        `json:"Id"`
	Name               string        `json:"Name"`
	SortName           string        `json:"SortName"`
	Overview           string        `json:"Overview"`
	Type               string        `json:"Type"`
	CollectionType     string        `json:"CollectionType,omitempty"` // For libraries: "movies", "tvshows"
	DateCreated        string        `json:"DateCreated,omitempty"`
	DateLastMediaAdded string        `json:"DateLastMediaAdded,omitempty"` // When last episode was added to show
	ProductionYear     int           `json:"ProductionYear,omitempty"`
	RunTimeTicks       int64         `json:"RunTimeTicks,omitempty"` // Duration in 100-nanosecond units
	CommunityRating    float64       `json:"CommunityRating,omitempty"`
	OfficialRating     string        `json:"OfficialRating,omitempty"`
	ImageTags          imageTags     `json:"ImageTags,omitempty"`
	ParentID           string        `json:"ParentId,omitempty"`
	SeriesID           string        `json:"SeriesId,omitempty"`
	SeriesName         string        `json:"SeriesName,omitempty"`
	SeasonID           string        `json:"SeasonId,omitempty"`
	SeasonName         string        `json:"SeasonName,omitempty"`
	ParentIndexNumber  int           `json:"ParentIndexNumber,omitempty"`  // Season number
	IndexNumber        int           `json:"IndexNumber,omitempty"`        // Episode number
	ChildCount         int           `json:"ChildCount,omitempty"`         // Number of child items (seasons for show, episodes for season)
	RecursiveItemCount int           `json:"RecursiveItemCount,omitempty"` // Total items recursively (episodes for show)
	PlaylistItemID     string        `json:"PlaylistItemId,omitempty"`     // Per-entry ID within a playlist
	UserData           *userData     `json:"UserData,omitempty"`
	MediaSources       []mediaSource `json:"MediaSources,omitempty"`
	Container          string        `json:"Container,omitempty"`
	MediaStreams       []mediaStream `json:"MediaStreams,omitempty"`
}

// imageTags contains image tag IDs for various image types
type imageTags struct {
	Primary string `json:"Primary,omitempty"`
	Thumb   string `json:"Thumb,omitempty"`
	Banner  string `json:"Banner,omitempty"`
	Logo    string `json:"Logo,omitempty"`
}

// userData contains user-specific data for an item (watch status, progress)
type userData struct {
	PlaybackPositionTicks int64  `json:"PlaybackPositionTicks"` // Progress in 100-nanosecond units
	PlayCount             int    `json:"PlayCount"`
	IsFavorite            bool   `json:"IsFavorite"`
	Played                bool   `json:"Played"`
	Key                   string `json:"Key"`
	UnplayedItemCount     int    `json:"UnplayedItemCount,omitempty"` // For containers like shows/seasons
}

// mediaSource represents a media source (file) for an item
type mediaSource struct {
	ID                   string        `json:"Id"`
	Path                 string        `json:"Path"`
	Protocol             string        `json:"Protocol"` // "File" or "Http"
	Type                 string        `json:"Type"`
	Container            string        `json:"Container"`
	Size                 int64         `json:"Size"`
	Name                 string        `json:"Name"`
	RunTimeTicks         int64         `json:"RunTimeTicks"`
	SupportsDirectPlay   bool          `json:"SupportsDirectPlay"`
	SupportsDirectStream bool          `json:"SupportsDirectStream"`
	SupportsTranscoding  bool          `json:"SupportsTranscoding"`
	MediaStreams         []mediaStream `json:"MediaStreams,omitempty"`
	DirectStreamURL      string        `json:"DirectStreamUrl,omitempty"`
}

// mediaStream represents a video, audio, or subtitle stream
type mediaStream struct {
	Codec        string `json:"Codec"`
	Language     string `json:"Language,omitempty"`
	DisplayTitle string `json:"DisplayTitle,omitempty"`
	Type         string `json:"Type"` // "Video", "Audio", "Subtitle"
	Index        int    `json:"Index"`
	IsDefault    bool   `json:"IsDefault"`
	Height       int    `json:"Height,omitempty"`
	Width        int    `json:"Width,omitempty"`
	BitRate      int    `json:"BitRate,omitempty"`
	Channels     int    `json:"Channels,omitempty"`
	SampleRate   int    `json:"SampleRate,omitempty"`
	AspectRatio  string `json:"AspectRatio,omitempty"`
}

// playbackInfoResponse contains playback information for an item
type playbackInfoResponse struct {
	MediaSources  []mediaSource `json:"MediaSources"`
	PlaySessionID string        `json:"PlaySessionId"`
}
