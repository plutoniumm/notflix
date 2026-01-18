package server

import (
	"fmt"
	"hash/fnv"
	"log"
	"os"
	"strconv"
	"strings"
)

func Hash(name string) string {
	h := fnv.New32a()
	h.Write([]byte(name))
	return fmt.Sprintf("%07d", h.Sum32()%1e7)
}

func EnsureDir(dir string) {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		err := os.Mkdir(dir, 0755)
		if err != nil {
			log.Fatalf("Failed to create directory %s: %v", dir, err)
		}
	}
}

func DelFile(filePath string) {
	err := os.Remove(filePath)
	if err != nil {
		log.Printf("Failed to delete file %s: %v", filePath, err)
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
