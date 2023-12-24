package server

import (
	"strconv"
	"strings"
)

func ParseRangeHeader(rangeHeader string, fileSize int64) (start int64, end int64, contentLength string) {
	rangeParts := strings.Split(rangeHeader, "=")
	byteRange := strings.Split(rangeParts[1], "-")

	start, _ = strconv.ParseInt(byteRange[0], 10, 64)
	if byteRange[1] != "" {
		end, _ = strconv.ParseInt(byteRange[1], 10, 64)
	} else {
		end = fileSize - 1
	}
	contentLength = strconv.FormatInt(end-start+1, 10)

	return start, end, contentLength
}
