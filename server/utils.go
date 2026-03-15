package server

import (
	"fmt"
	"hash/fnv"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	astiffprobe "github.com/asticode/go-astiffprobe"
)

var prober = astiffprobe.New(astiffprobe.Configuration{BinaryPath: "ffprobe"})

func Error(message string, c *gin.Context, statusCode int) {
	fmt.Println("Error:", message)
	c.String(statusCode, message)
}

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

func DelFile(path string) {
	if err := os.Remove(path); err != nil {
		log.Printf("Failed to delete file %s: %v", path, err)
	}
}

func Range(hdr string, size int64) (start int64, end int64, clen string) {
	parts := strings.Split(hdr, "=")
	lr := strings.Split(parts[1], "-")

	start, _ = strconv.ParseInt(lr[0], 10, 64)
	if lr[1] != "" {
		end, _ = strconv.ParseInt(lr[1], 10, 64)
	} else {
		end = size - 1
	}
	clen = strconv.FormatInt(end-start+1, 10)

	return start, end, clen
}
