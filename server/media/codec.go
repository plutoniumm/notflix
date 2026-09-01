package media

import (
	"path/filepath"

	"notflix/server/library"
)

const (
	CodecH264 = "h264"
	CodecAV1  = "av1"
)

func codecKey(file string) string {
	return "codec:" + library.Hash(file)
}

// MediaCodec falls back to the BASENAME hash: marks carry the rel path from
// conversion time and CleanAll/Organize move files later, orphaning dir keys.
func MediaCodec(file string) string {
	v, _ := library.KVGetValue(codecKey(file)).(string)
	if v == "" {
		v, _ = library.KVGetValue(codecKey(filepath.Base(file))).(string)
	}
	if v == CodecAV1 {
		return CodecAV1
	}
	return CodecH264
}

func SetMediaCodec(file, codec string) error {
	return library.KVSetValue(codecKey(filepath.Base(file)), codec)
}
