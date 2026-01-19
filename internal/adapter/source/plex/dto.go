package plex

// MediaContainer is the root container for Plex API responses
type MediaContainer struct {
	Size                int         `json:"size"`
	TotalSize           int         `json:"totalSize,omitempty"`
	Offset              int         `json:"offset,omitempty"`
	AllowSync           bool        `json:"allowSync,omitempty"`
	Identifier          string      `json:"identifier,omitempty"`
	LibrarySectionID    int         `json:"librarySectionID,omitempty"`
	LibrarySectionTitle string      `json:"librarySectionTitle,omitempty"`
	LibrarySectionUUID  string      `json:"librarySectionUUID,omitempty"`
	MediaTagPrefix      string      `json:"mediaTagPrefix,omitempty"`
	MediaTagVersion     int         `json:"mediaTagVersion,omitempty"`
	Directory           []Directory `json:"Directory,omitempty"`
	Metadata            []Metadata  `json:"Metadata,omitempty"`
}

// Directory represents a library section
type Directory struct {
	AllowSync        bool   `json:"allowSync,omitempty"`
	Art              string `json:"art,omitempty"`
	Composite        string `json:"composite,omitempty"`
	Filters          bool   `json:"filters,omitempty"`
	Refreshing       bool   `json:"refreshing,omitempty"`
	Thumb            string `json:"thumb,omitempty"`
	Key              string `json:"key"`
	Type             string `json:"type"`
	Title            string `json:"title"`
	Agent            string `json:"agent,omitempty"`
	Scanner          string `json:"scanner,omitempty"`
	Language         string `json:"language,omitempty"`
	UUID             string `json:"uuid,omitempty"`
	UpdatedAt        int64  `json:"updatedAt,omitempty"`
	CreatedAt        int64  `json:"createdAt,omitempty"`
	ScannedAt        int64  `json:"scannedAt,omitempty"`
	Content          bool   `json:"content,omitempty"`
	Directory        bool   `json:"directory,omitempty"`
	ContentChangedAt int64  `json:"contentChangedAt,omitempty"`
	Hidden           int    `json:"hidden,omitempty"`
}

// Metadata represents a media item (movie, show, season, or episode)
type Metadata struct {
	RatingKey             string  `json:"ratingKey"`
	Key                   string  `json:"key"`
	ParentRatingKey       string  `json:"parentRatingKey,omitempty"`
	GrandparentRatingKey  string        `json:"grandparentRatingKey,omitempty"`
	GUID                  string        `json:"guid,omitempty"`
	GuidList              []interface{} `json:"Guid,omitempty"`
	Studio                string        `json:"studio,omitempty"`
	Type                  string  `json:"type"`
	Title                 string  `json:"title"`
	GrandparentKey        string  `json:"grandparentKey,omitempty"`
	ParentKey             string  `json:"parentKey,omitempty"`
	GrandparentTitle      string  `json:"grandparentTitle,omitempty"`
	ParentTitle           string  `json:"parentTitle,omitempty"`
	ContentRating         string  `json:"contentRating,omitempty"`
	Summary               string  `json:"summary,omitempty"`
	Index                 int     `json:"index,omitempty"`
	ParentIndex           int     `json:"parentIndex,omitempty"`
	Rating                float64       `json:"rating,omitempty"`
	RatingList            []interface{} `json:"Rating,omitempty"`
	AudienceRating        float64       `json:"audienceRating,omitempty"`
	ViewOffset            int     `json:"viewOffset,omitempty"`
	LastViewedAt          int64   `json:"lastViewedAt,omitempty"`
	Year                  int     `json:"year,omitempty"`
	Tagline               string  `json:"tagline,omitempty"`
	Thumb                 string  `json:"thumb,omitempty"`
	Art                   string  `json:"art,omitempty"`
	ParentThumb           string  `json:"parentThumb,omitempty"`
	GrandparentThumb      string  `json:"grandparentThumb,omitempty"`
	GrandparentArt        string  `json:"grandparentArt,omitempty"`
	Duration              int     `json:"duration,omitempty"`
	OriginallyAvailableAt string  `json:"originallyAvailableAt,omitempty"`
	AddedAt               int64   `json:"addedAt,omitempty"`
	UpdatedAt             int64   `json:"updatedAt,omitempty"`
	TitleSort             string  `json:"titleSort,omitempty"`
	ViewCount             int     `json:"viewCount,omitempty"`
	ChildCount            int     `json:"childCount,omitempty"`
	LeafCount             int     `json:"leafCount,omitempty"`
	ViewedLeafCount       int     `json:"viewedLeafCount,omitempty"`
	LibrarySectionID      int     `json:"librarySectionID,omitempty"`
	LibrarySectionKey     string        `json:"librarySectionKey,omitempty"`
	LibrarySectionTitle   string        `json:"librarySectionTitle,omitempty"`
	Media                 []Media       `json:"Media,omitempty"`
	Image                 []interface{} `json:"Image,omitempty"`
	Genre                 []interface{} `json:"Genre,omitempty"`
	Country               []interface{} `json:"Country,omitempty"`
	Director              []interface{} `json:"Director,omitempty"`
	Writer                []interface{} `json:"Writer,omitempty"`
	Role                  []interface{} `json:"Role,omitempty"`
	Producer              []interface{} `json:"Producer,omitempty"`
}

// Media represents media information (video streams, codecs, etc.)
type Media struct {
	ID                    int    `json:"id"`
	Duration              int    `json:"duration,omitempty"`
	Bitrate               int    `json:"bitrate,omitempty"`
	Width                 int    `json:"width,omitempty"`
	Height                int    `json:"height,omitempty"`
	AspectRatio           interface{} `json:"aspectRatio,omitempty"`
	AudioChannels         int    `json:"audioChannels,omitempty"`
	AudioCodec            string `json:"audioCodec,omitempty"`
	VideoCodec            string `json:"videoCodec,omitempty"`
	VideoResolution       string `json:"videoResolution,omitempty"`
	Container             string `json:"container,omitempty"`
	VideoFrameRate        string `json:"videoFrameRate,omitempty"`
	AudioProfile          string `json:"audioProfile,omitempty"`
	VideoProfile          string `json:"videoProfile,omitempty"`
	Part                  []Part `json:"Part,omitempty"`
}

// Part represents a media file part
type Part struct {
	ID           int           `json:"id"`
	Key          string        `json:"key"`
	Duration     int           `json:"duration,omitempty"`
	File         string        `json:"file,omitempty"`
	Size         int64         `json:"size,omitempty"`
	AudioProfile string        `json:"audioProfile,omitempty"`
	Container    string        `json:"container,omitempty"`
	VideoProfile string        `json:"videoProfile,omitempty"`
	HasThumbnail string        `json:"hasThumbnail,omitempty"`
	Stream       []interface{} `json:"Stream,omitempty"`
}

// APIResponse wraps the MediaContainer for JSON unmarshaling
type APIResponse struct {
	MediaContainer MediaContainer `json:"MediaContainer"`
}

// PINResponse represents the response from PIN generation
type PINResponse struct {
	ID          int    `json:"id"`
	Code        string `json:"code"`
	Product     string `json:"product"`
	Trusted     bool   `json:"trusted"`
	ClientID    string `json:"clientIdentifier"`
	AuthToken   string `json:"authToken,omitempty"`
	ExpiresAt   string `json:"expiresAt"`
}

// PINCheckResponse represents the response from PIN check
type PINCheckResponse struct {
	ID        int    `json:"id"`
	Code      string `json:"code"`
	AuthToken string `json:"authToken"`
	ExpiresAt string `json:"expiresAt"`
}

// UserResponse represents user information
type UserResponse struct {
	ID       int    `json:"id"`
	UUID     string `json:"uuid"`
	Username string `json:"username"`
	Email    string `json:"email"`
	Thumb    string `json:"thumb"`
	Title    string `json:"title"`
}

// ServerResource represents a server from plex.tv resources
type ServerResource struct {
	Name        string       `json:"name"`
	Product     string       `json:"product"`
	ProductVersion string   `json:"productVersion"`
	Platform    string       `json:"platform"`
	ClientID    string       `json:"clientIdentifier"`
	Owned       bool         `json:"owned"`
	AccessToken string       `json:"accessToken"`
	Connections []Connection `json:"connections"`
}

// Connection represents a server connection option
type Connection struct {
	Protocol string `json:"protocol"`
	Address  string `json:"address"`
	Port     int    `json:"port"`
	URI      string `json:"uri"`
	Local    bool   `json:"local"`
	Relay    bool   `json:"relay"`
	IPv6     bool   `json:"IPv6"`
}
