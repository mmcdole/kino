// Package normalize turns backend media metadata into consistent display values.
package normalize

import "strings"

// ContentRating maps "not rated" variants to NR.
func ContentRating(rating string) string {
	switch strings.ToLower(rating) {
	case "not rated", "unrated":
		return "NR"
	default:
		return rating
	}
}

// Container returns the first of a possibly comma-separated container list
// (e.g. "mov,mp4,m4a,3gp,3g2,mj2"), lowercased.
func Container(container string) string {
	if container == "" {
		return ""
	}
	if i := strings.Index(container, ","); i >= 0 {
		container = container[:i]
	}
	return strings.ToLower(container)
}

// VideoCodec returns the display name of a video codec.
func VideoCodec(codec string) string {
	switch strings.ToLower(codec) {
	case "hevc", "h265":
		return "HEVC"
	case "h264", "avc":
		return "H.264"
	case "mpeg4":
		return "MPEG4"
	case "vc1":
		return "VC-1"
	case "vp9":
		return "VP9"
	case "av1":
		return "AV1"
	default:
		return strings.ToUpper(codec)
	}
}

// AudioCodec returns the display name of an audio codec.
func AudioCodec(codec string) string {
	switch strings.ToLower(codec) {
	case "aac":
		return "AAC"
	case "ac3":
		return "AC3"
	case "eac3":
		return "EAC3"
	case "dca", "dts":
		return "DTS"
	case "truehd":
		return "TrueHD"
	case "flac":
		return "FLAC"
	case "mp3":
		return "MP3"
	case "opus":
		return "Opus"
	case "vorbis":
		return "Vorbis"
	default:
		return strings.ToUpper(codec)
	}
}
