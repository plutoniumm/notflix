package media

import (
	"notflix/server/library"
)

// Codec preference is per-video, persisted in the KV store under "codec:<hash>".
// Default ("") means h264 — the legacy path. Newly converted files are stamped
// "av1" by convert.go so they take the new fMP4+AV1 HLS path. Existing library
// stays grandfathered on h264 (no marker → default).
//
// Manual override is available via the existing /kv/set HTTP endpoint:
// POST /kv/set {"key":"codec:<hash>","value":"av1"}.

const (
	CodecH264 = "h264"
	CodecAV1  = "av1"
)

func codecKey(file string) string {
	return "codec:" + library.Hash(file)
}

// MediaCodec returns "h264" or "av1" for the given video file (relative path
// as passed through the HLS endpoints). Anything other than "av1" in KV maps
// to h264, including missing entries.
func MediaCodec(file string) string {
	v, _ := library.KVGetValue(codecKey(file)).(string)
	if v == CodecAV1 {
		return CodecAV1
	}
	return CodecH264
}

// SetMediaCodec stamps the codec preference for the given file. Errors are
// logged at the call site if needed; callers typically fire-and-forget.
func SetMediaCodec(file, codec string) error {
	return library.KVSetValue(codecKey(file), codec)
}
