package main

import (
	"fmt"
	"log"
	"os"

	server "notflix/server"

	"github.com/fasthttp/router"
	"github.com/valyala/fasthttp"
)

func trackList() []string {
	files, err := os.ReadDir("./video/")
	if err != nil {
		log.Fatal(err)
	}

	var names []string
	for _, file := range files {
		if file.IsDir() {
			continue
		}

		if file.Name()[len(file.Name())-4:] != ".mp4" {
			continue
		}

		names = append(names, file.Name())
	}

	return names
}

// save error to errors
func Error(ctx *fasthttp.RequestCtx) {
	ctx.SetContentType("text/plain")
	ctx.SetStatusCode(fasthttp.StatusOK)

	err := os.WriteFile("./config/errors", ctx.PostBody(), 0644)
	if err != nil {
		fmt.Fprintf(ctx, "error")
		return
	}

	fmt.Fprintf(ctx, "success")
}

func Action(ctx *fasthttp.RequestCtx) {
	ctx.SetContentType("text/plain")
	ctx.SetStatusCode(fasthttp.StatusOK)

	action, err := os.ReadFile("./config/action")
	if err != nil {
		fmt.Fprintf(ctx, "error")
		return
	}
	if len(action) < 2 {
		return
	}

	fmt.Fprintf(ctx, string(action))
	os.WriteFile("./action", []byte(""), 0644)
}

// IF YOU GET A READ SOCKET ERROR: TURN OFF THE FIREWALL
func main() {
	r := router.New()
	hub := server.NewHub()

	go hub.Run()

	r.RedirectTrailingSlash = true

	PORT := os.Getenv("PORT")
	if PORT == "" {
		PORT = "5173"
	}

	r.ServeFiles("/subs/{filepath:*}", "./subs/")
	r.ServeFiles("/assets/{filepath:*}", "./dist/assets/")

	r.GET("/", server.GETIndex)
	r.GET("/video/{name}", server.GETVideo)
	r.GET("/ws", func(ctx *fasthttp.RequestCtx) {
		server.ServeWs(ctx, hub)
	})
	r.GET("/list", func(ctx *fasthttp.RequestCtx) {
		ctx.SetContentType("application/json")
		ctx.SetStatusCode(fasthttp.StatusOK)

		names := trackList()
		fmt.Fprintf(ctx, "[")
		for i, name := range names {
			fmt.Fprintf(ctx, "\"%s\"", name)
			if i != len(names)-1 {
				fmt.Fprintf(ctx, ",")
			}
		}
		fmt.Fprintf(ctx, "]")
	})

	// /action reads file action and sends text
	r.GET("/action", Action)
	r.POST("/error", Error)

	log.Println("Server started on localhost:" + PORT)
	log.Fatal(fasthttp.ListenAndServe("0.0.0.0:"+PORT, r.Handler))
}
