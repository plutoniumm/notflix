package main

import (
	"fmt"
	"log"
	// "io/ioutil"

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

func Hello(ctx *fasthttp.RequestCtx) {
	fmt.Fprintf(ctx, "Hello, %s!\n", ctx.UserValue("name"))
}

func main() {
	r := router.New()

	r.ServeFiles("/assets/{filepath:*}", "./assets/")

	r.GET("/", Index);
	r.GET("/hello/{name}", Hello);

	// print
	log.Println("Server started on localhost:3000")
	log.Fatal(fasthttp.ListenAndServe(":3000", r.Handler))
}