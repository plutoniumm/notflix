package main

import (
	"fmt"
	"log"
	"os"

	server "notflix/server"

	"github.com/fasthttp/router"
	"github.com/valyala/fasthttp"
)

func postTracker(ctx *fasthttp.RequestCtx) {
	ctx.SetContentType("text/plain; charset=utf-8")
	ctx.SetStatusCode(fasthttp.StatusOK)

	name := ctx.UserValue("name").(string)
	body := string(ctx.PostBody())

	fmt.Fprintf(ctx, "name: %s\n", name)
	fmt.Fprintf(ctx, "body %s\n", body)

	f, err := os.ReadFile("./tests.txt")
	if err != nil {
		fmt.Fprintf(ctx, "Error reading tests.txt\n")
	}

	err = os.WriteFile("./tests.txt", []byte(string(f)+name+"\n"), 0644)
	if err != nil {
		fmt.Fprintf(ctx, "Error writing to tests.txt\n")
	}

	fmt.Fprintf(ctx, "tests.txt updated\n")
}

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

// IF YOU GET A READ SOCKET ERROR: TURN OFF THE FIREWALL
func main() {
	r := router.New()
	hub := server.NewHub()

	go hub.Run()

	r.RedirectTrailingSlash = true

	PORT := os.Getenv("PORT")
	if PORT == "" {
		PORT = "3000"
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

	r.POST("/track/{name}", postTracker)

	log.Println("Server started on localhost:" + PORT)
	log.Fatal(fasthttp.ListenAndServe("0.0.0.0:"+PORT, r.Handler))
}
