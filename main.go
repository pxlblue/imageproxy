package main

import (
	"context"
	"database/sql"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path"

	"cloud.google.com/go/storage"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	"github.com/valyala/fasthttp"
	"google.golang.org/api/option"
)

var client *storage.Client
var database *sql.DB

func requestHandler(ctx *fasthttp.RequestCtx) {
	requestPath := string(ctx.Path())

	if requestPath == "/" {
		ctx.Redirect("https://pxl.blue/?utm_source=proxy", 301)
	} else if path.Ext(requestPath) != "" {
		// Request file from GCS
		rc, err := client.Bucket(os.Getenv("STORAGE_BUCKET")).Object(path.Base(requestPath)).NewReader(context.Background())
		if err != nil {
			ctx.Error("Image not found", 404)
			ctx.Done()
			return
		}
		defer rc.Close()
		data, err := ioutil.ReadAll(rc)
		if err != nil {
			ctx.Error(fmt.Sprintf("Error reading file from Storage (ioutil.ReadAll failed) (error: %v)", err), 500)
			ctx.Done()
			return
		}
		ctx.Response.Header.SetContentType(rc.Attrs.ContentType)

		ctx.SetBody(data)
		ctx.Done()
	} else {
		base := path.Base(requestPath)
		var destination string
		err := database.QueryRow("SELECT \"destination\" FROM pxl.public.\"short_url\" WHERE \"shortId\" = $1", base).Scan(&destination)
		if err != nil {
			ctx.Error("Short URL not found", 404)
			ctx.Done()
			return
		}
		ctx.Redirect(destination, 301)
	}
}

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Printf("Error in godotenv.Load(): %v", err)
	}

	db, err := sql.Open("postgres", os.Getenv("PG_CONNSTRING"))
	database = db

	if err != nil {
		log.Fatalf("Error in sql.Open: %v", err)
	}

	bgCtx := context.Background()
	gcsClient, err := storage.NewClient(bgCtx, option.WithCredentialsJSON([]byte(os.Getenv("STORAGE_ACCOUNT"))))
	if err != nil {
		log.Fatalf("Error in storage.NewClient: %v", err)
	}
	client = gcsClient
	defer gcsClient.Close()

	handler := fasthttp.CompressHandler(requestHandler)

	if err := fasthttp.ListenAndServe(":"+os.Getenv("PORT"), handler); err != nil {
		log.Fatalf("Error in ListenAndServe: %v", err)
	}

	log.Printf("Listening on port %s", os.Getenv("PORT"))
}
