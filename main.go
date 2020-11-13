package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path"
	"strings"

	"cloud.google.com/go/storage"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	"github.com/valyala/fasthttp"
	"google.golang.org/api/option"
)

var client *storage.Client
var database *sql.DB
var template string = "<!DOCTYPE html><html style=\"height:100%%\"><head><meta name=\"viewport\" content=\"width=device-width,minimum-scale=0.1\"><meta name=\"twitter:card\" content=\"summary_large_image\"><meta property=\"og:description\" content=\"%s\"><meta property=\"twitter:image\" content=\"%s\"><meta name=\"theme-color\" content=\"%s\"><link type=\"application/json+oembed\" href=\"%s\"></head><body style=\"margin: 0px;background: #0e0e0e;display:flex;justify-content:center;height:100%%\"><img style=\"-webkit-user-select:none;margin:auto\" src=\"%s\"></body></html>"

// OEmbed smthn oembed stuff
type OEmbed struct {
	Type    string `json:"type"`
	Version string `json:"version"`
	Title   string `json:"title"`
	Author  string `json:"author_name,omitempty"`
}

func requestHandler(ctx *fasthttp.RequestCtx) {
	requestPath := string(ctx.Path())
	base := path.Base(requestPath)
	host := "https://" + string(ctx.Request.Header.Peek("X-Forwarded-Host"))

	if requestPath == "/" {
		ctx.Redirect("https://pxl.blue/?utm_source=proxy", 301)
	} else if path.Ext(requestPath) == ".json" && strings.HasPrefix(base, "em") && !strings.Contains(requestPath, "raw") {
		// oembed route
		var (
			embed            bool
			embedAuthor      bool
			embedAuthorStr   string
			embedTitle       string
			embedDescription string
			embedColor       string
		)
		imgPath := base[0 : len(base)-5]
		err := database.QueryRow("SELECT \"embed\", \"embedAuthor\", \"embedAuthorStr\", \"embedTitle\", \"embedDescription\", \"embedColor\" FROM pxl.public.\"image\" WHERE \"path\" = $1", imgPath).Scan(&embed, &embedAuthor, &embedAuthorStr, &embedTitle, &embedDescription, &embedColor)
		if err != nil {
			ctx.Error("Image not found in database", 404)
			ctx.Done()
			return
		}
		oembed := OEmbed{Type: "link", Version: "1.0", Title: embedTitle, Author: embedAuthorStr}
		if !embedAuthor {
			oembed.Author = ""
		}
		var embedData []byte
		embedData, err = json.Marshal(oembed)
		if err != nil {
			log.Fatalf("Error in json.Marshal: %v", err)
		}
		ctx.SetBody(embedData)
		ctx.SetContentType("application/json; charset=utf-8")
		ctx.Done()
	} else if path.Ext(requestPath) != "" && strings.HasPrefix(base, "em") && !strings.Contains(requestPath, "raw") {
		var (
			embed            bool
			embedDescription string
			embedColor       string
		)
		err := database.QueryRow("SELECT \"embed\", \"embedDescription\", \"embedColor\" FROM pxl.public.\"image\" WHERE \"path\" = $1", base).Scan(&embed, &embedDescription, &embedColor)
		if err != nil {
			ctx.Error("Image not found in database", 404)
			ctx.Done()
			return
		}
		if !embed {
			ctx.Redirect("https://"+string(ctx.Host())+"/raw/"+base, 301)
			return
		}
		ctx.SetContentType("text/html; charset=utf-8")
		imgURL := fmt.Sprintf("%s/raw/%s", host, base)
		html := fmt.Sprintf(template, embedDescription, imgURL, embedColor, fmt.Sprintf("%s/%s.json", host, base), imgURL)
		ctx.SetBody([]byte(html))
		ctx.Done()
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
		ctx.SetContentType(rc.Attrs.ContentType)

		ctx.SetBody(data)
		ctx.Done()
	} else {
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
