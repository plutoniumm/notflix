package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/fasthttp/router"
	"github.com/valyala/fasthttp"
)

func getIndex(ctx *fasthttp.RequestCtx) {
	ctx.SetContentType("text/html; charset=utf-8")
	ctx.SetStatusCode(fasthttp.StatusOK)

	csp := []string{
		"default-src *  data: 'unsafe-inline' 'unsafe-eval';",
		"script-src * data: 'unsafe-inline' 'unsafe-eval';",
		"img-src * data: 'unsafe-inline';",
		"font-src * data: 'unsafe-inline' 'unsafe-eval';",
		"worker-src * data: blob: 'unsafe-inline' 'unsafe-eval'",
	}
	CSP := strings.Join(csp, " ")

	ctx.Response.Header.Set("Content-Security-Policy", CSP)
	ctx.Response.Header.Set("X-Host", "Google Golang")

	ctx.SendFile("./index.html")
}

func getMovies(ctx *fasthttp.RequestCtx) {
	ctx.SetContentType("application/json; charset=utf-8")
	ctx.SetStatusCode(fasthttp.StatusOK)

	files, err := ioutil.ReadDir("./video/")
	if err != nil {
		log.Fatal(err)
	}

	var movieList []string
	for _, f := range files {
		movieList = append(movieList, f.Name())
	}

	jsonData, err := json.Marshal(movieList)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Fprintf(ctx, "%s", jsonData)
}

// get {name} & body and append to track.txt
func postTracker(ctx *fasthttp.RequestCtx) {
	ctx.SetContentType("text/plain; charset=utf-8")
	ctx.SetStatusCode(fasthttp.StatusOK)

	name := ctx.UserValue("name").(string)
	body := string(ctx.PostBody())

	fmt.Fprintf(ctx, "name: %s\n", name)
	fmt.Fprintf(ctx, "body %s\n", body)

	f, err := ioutil.ReadFile("./tests.txt")
	if err != nil {
		log.Fatal(err)
	}

	err = ioutil.WriteFile("./tests.txt", []byte(string(f)+name+"\n"), 0644)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Fprintf(ctx, "tests.txt updated\n")
}

func getVideo(ctx *fasthttp.RequestCtx) {
	id := ctx.UserValue("name").(string)
	filepath := filepath.Join("video", id)
	fileInfo, err := os.Stat(filepath)
	if err != nil {
		ctx.Error("File not found", fasthttp.StatusNotFound)
		return
	}

	fileSize := fileInfo.Size()
	rangeHeader := string(ctx.Request.Header.Peek("Range"))

	var headers fasthttp.ResponseHeader
	var stream *os.File

	if rangeHeader != "" {
		start, end, contentLength := parseRangeHeader(rangeHeader, fileSize)
		headers.Set(
			"Content-Range",
			"bytes "+strconv.FormatInt(start, 10)+"-"+strconv.FormatInt(end, 10)+"/"+strconv.FormatInt(fileSize, 10),
		)
		headers.Set("Content-Type", "video/mp4")
		headers.Set("Accept-Ranges", "bytes")
		headers.Set("Content-Length", contentLength)
		stream, _ = os.Open(filepath)
		stream.Seek(start, 0)
	} else {
		headers.Set("Content-Type", "video/mp4")
		headers.Set("Content-Length", strconv.FormatInt(fileSize, 10))
		stream, _ = os.Open(filepath)
	}

	ctx.SetStatusCode(fasthttp.StatusPartialContent)
	ctx.Response.Header = headers
	ctx.SendFile(filepath)
	return
}

func parseRangeHeader(rangeHeader string, fileSize int64) (start int64, end int64, contentLength string) {
	rangeParts := strings.Split(rangeHeader, "=")
	byteRange := strings.Split(rangeParts[1], "-")

	start, _ = strconv.ParseInt(byteRange[0], 10, 64)
	if byteRange[1] != "" {
		end, _ = strconv.ParseInt(byteRange[1], 10, 64)
	} else {
		end = fileSize - 1
	}
	contentLength = strconv.FormatInt(end-start+1, 10)
	return
}

func main() {
	r := router.New()
	r.RedirectTrailingSlash = true

	r.ServeFiles("/assets/{filepath:*}", "./assets/")

	r.GET("/", getIndex)
	r.GET("/list", getMovies)
	r.GET("/video/{name}", getVideo)

	r.POST("/track/{name}", postTracker)

	log.Println("Server started on localhost:3000")
	log.Fatal(fasthttp.ListenAndServe(":3000", r.Handler))
}
