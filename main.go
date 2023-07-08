package main

import (
	"encoding/json"
	"fmt"
	"log"

	"io/ioutil"

	"github.com/fasthttp/router"
	"github.com/valyala/fasthttp"
)

func Index(ctx *fasthttp.RequestCtx) {
	ctx.SetContentType("text/html; charset=utf-8")
	ctx.SetStatusCode(fasthttp.StatusOK)

	// ctx.Response.Header.Set("Content-Security-Policy", "")
	ctx.Response.Header.Set("X-Host", "Google Golang")

	ctx.SendFile("./index.html")
}

func MovieList(ctx *fasthttp.RequestCtx) {
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

func Hello(ctx *fasthttp.RequestCtx) {
	fmt.Fprintf(ctx, "Hello, %s!\n", ctx.UserValue("name"))
}

func main() {
	r := router.New()
	r.RedirectTrailingSlash = true

	r.ServeFiles("/assets/{filepath:*}", "./assets/")

	r.GET("/", Index)
	r.GET("/list", MovieList)
	r.GET("/hello/{name}", Hello)

	// print
	log.Println("Server started on localhost:3000")
	log.Fatal(fasthttp.ListenAndServe(":3000", r.Handler))
}
