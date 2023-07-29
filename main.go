package notflix

import (
	"fmt"
	"io/ioutil"
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

	f, err := ioutil.ReadFile("./tests.txt")
	if err != nil {
		fmt.Fprintf(ctx, "Error reading tests.txt\n")
	}

	err = ioutil.WriteFile("./tests.txt", []byte(string(f)+name+"\n"), 0644)
	if err != nil {
		fmt.Fprintf(ctx, "Error writing to tests.txt\n")
	}

	fmt.Fprintf(ctx, "tests.txt updated\n")
}

func main() {
	r := router.New()
	r.RedirectTrailingSlash = true

	var PORT string
	if len(os.Args) > 1 {
		PORT = os.Args[1]
	} else {
		PORT = "3000"
	}

	r.ServeFiles("/assets/{filepath:*}", "./assets/")

	r.GET("/", server.GETIndex)
	r.GET("/video/{name}", server.GETVideo)

	r.POST("/track/{name}", postTracker)

	log.Println("Server started on localhost:" + PORT)
	log.Fatal(fasthttp.ListenAndServe(":"+PORT, r.Handler))
}
