package kivo

import "time"

// serverInfo is GET /api/v1/server, which answers signed out.
type serverInfo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

// userInfo is GET /api/v1/users/me.
type userInfo struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

type libraryList struct {
	Items []library `json:"items"`
}

type library struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"` // movies, shows, videos, audiobooks
	WorkCount int    `json:"workCount"`
}

// workPage holds queried works or children. Total is on a query's first page only.
type workPage struct {
	Items []work `json:"items"`
	Total *int   `json:"total"`
}

type workQueryRequest struct {
	Query   workQuery `json:"query"`
	Library string    `json:"library"`
	Offset  int       `json:"offset"`
	Limit   int       `json:"limit,omitempty"`
}

type workQuery struct {
	Kinds []string `json:"kinds"`
}

// work is a WorkSummary card; kind-specific data is under the kind's name.
type work struct {
	ID              string     `json:"id"`
	Kind            string     `json:"kind"`
	Title           string     `json:"title"`
	Year            int        `json:"year"`
	DurationMs      int64      `json:"durationMs"`
	CommunityRating float64    `json:"communityRating"`
	ContentRating   string     `json:"contentRating"`
	AddedAt         time.Time  `json:"addedAt"`
	UserState       *userState `json:"userState"`
	Series          *struct {
		SeasonCount   int `json:"seasonCount"` // detail only
		UnplayedCount int `json:"unplayedCount"`
	} `json:"series"`
	Season *struct {
		Number        int    `json:"number"`
		SeriesID      string `json:"seriesId"`
		SeriesTitle   string `json:"seriesTitle"`
		EpisodeCount  int    `json:"episodeCount"`
		UnplayedCount int    `json:"unplayedCount"`
	} `json:"season"`
	Episode *struct {
		Season      int    `json:"seasonNumber"`
		Number      int    `json:"number"`
		SeriesID    string `json:"seriesId"`
		SeriesTitle string `json:"seriesTitle"`
		SeasonID    string `json:"seasonId"`
	} `json:"episode"`
}

type userState struct {
	Played     bool  `json:"played"`
	PositionMs int64 `json:"positionMs"`
}

type userStateUpdate struct {
	Played bool `json:"played"`
}

// playCreate is POST /api/v1/plays.
type playCreate struct {
	WorkID       string       `json:"workId"`
	PositionMs   int64        `json:"positionMs"`
	Capabilities capabilities `json:"capabilities"`
}

type capabilities struct {
	File      fileCapabilities     `json:"file"`
	Video     []videoCapability    `json:"video"`
	Audio     []audioCapability    `json:"audio"`
	Subtitles []subtitleCapability `json:"subtitles"`
}

type fileCapabilities struct {
	Containers []string `json:"containers"`
}

type videoCapability struct {
	Codec       string   `json:"codec"`
	Delivery    []string `json:"delivery"`
	MaxBitDepth int      `json:"maxBitDepth,omitempty"`
	HDR         []string `json:"hdr,omitempty"`
}

type audioCapability struct {
	Codec       string   `json:"codec"`
	Delivery    []string `json:"delivery"`
	MaxChannels int      `json:"maxChannels"`
}

type subtitleCapability struct {
	Codec    string   `json:"codec"`
	Delivery []string `json:"delivery"`
}

// playStarted is the response to starting a play or falling back. Media URLs
// are relative to /api/v1/.
type playStarted struct {
	ID         string `json:"id"`
	StartMs    int64  `json:"startMs"`
	DurationMs int64  `json:"durationMs"`
	Delivery   string `json:"delivery"` // file or stream
	URL        string `json:"url"`
	Parts      []struct {
		DurationMs int64  `json:"durationMs"`
		URL        string `json:"url"` // file delivery only
	} `json:"parts"`
}

// playUpdate is the heartbeat, PATCH /api/v1/plays/{id}.
type playUpdate struct {
	PositionMs         int64  `json:"positionMs"`
	State              string `json:"state"`
	SeekToFirstFrameMs int64  `json:"seekToFirstFrameMs,omitempty"`
}

type playStop struct {
	PositionMs int64 `json:"positionMs"`
}

type fallbackRequest struct {
	PositionMs int64  `json:"positionMs"`
	Code       string `json:"code"`
	Detail     string `json:"detail,omitempty"`
}

type deviceCodeStart struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	ClientID string `json:"clientId"`
}

type deviceCode struct {
	Code      string    `json:"code"`
	Secret    string    `json:"secret"`
	ExpiresAt time.Time `json:"expiresAt"`
	Interval  int       `json:"interval"`
	LinkURL   string    `json:"linkUrl"`
}

type deviceCodePoll struct {
	Secret string `json:"secret"`
}

// session is the response to an approved device code; Token is set only
// there.
type session struct {
	Token string `json:"token"`
}

// problem is an RFC 9457 problem document; Code is what clients translate.
type problem struct {
	Status int    `json:"status"`
	Code   string `json:"code"`
	Reason string `json:"reason"`
	Plays  []struct {
		Title  string `json:"title"`
		Client string `json:"client"`
	} `json:"plays"`
}
