package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"

	"github.com/fasthttp/router"
	"github.com/valyala/fasthttp"
)

func getIndex(ctx *fasthttp.RequestCtx) {
	ctx.SetContentType("text/html; charset=utf-8")
	ctx.SetStatusCode(fasthttp.StatusOK)

	// 	export const CSP = [
	//   "default-src *  data: 'unsafe-inline' 'unsafe-eval';",
	//   "script-src * data: 'unsafe-inline' 'unsafe-eval';",
	//   "img-src * data: 'unsafe-inline';",
	//   "font-src * data: 'unsafe-inline' 'unsafe-eval';",
	//   "worker-src * data: blob: 'unsafe-inline' 'unsafe-eval'"
	// ].join( " " );

	// ctx.Response.Header.Set("Content-Security-Policy", "")
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

	// append to track.txt
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

func main() {
	r := router.New()
	r.RedirectTrailingSlash = true

	r.ServeFiles("/assets/{filepath:*}", "./assets/")

	r.GET("/", getIndex)
	r.GET("/list", getMovies)

	r.POST("/track/{name}", postTracker)

	// print
	log.Println("Server started on localhost:3000")
	log.Fatal(fasthttp.ListenAndServe(":3000", r.Handler))
}
