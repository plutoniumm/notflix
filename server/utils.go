package server

import (
	"log"
	"os"
	"strconv"
	"strings"
)

func EnsureDir(dir string) {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		err := os.Mkdir(dir, 0755)
		if err != nil {
			log.Fatalf("Failed to create directory %s: %v", dir, err)
		}
	}
}

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
