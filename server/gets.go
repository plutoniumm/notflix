package server

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/valyala/fasthttp"
)

func GETIndex(ctx *fasthttp.RequestCtx) {
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

	return
}

func GETVideo(ctx *fasthttp.RequestCtx) {
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

	headers.Set("Content-Type", "video/mp4")

	if rangeHeader != "" {
		start, end, contentLength := ParseRangeHeader(rangeHeader, fileSize)
		headers.Set(
			"Content-Range",
			"bytes "+strconv.FormatInt(start, 10)+"-"+strconv.FormatInt(end, 10)+"/"+strconv.FormatInt(fileSize, 10),
		)

		headers.Set("Accept-Ranges", "bytes")
		headers.Set("Content-Length", contentLength)
		stream, _ = os.Open(filepath)
		stream.Seek(start, 0)
	} else {
		headers.Set("Content-Length", strconv.FormatInt(fileSize, 10))
		stream, _ = os.Open(filepath)
	}

	ctx.SetStatusCode(fasthttp.StatusPartialContent)
	ctx.Response.Header = headers
	ctx.SendFile(filepath)

	return
}
