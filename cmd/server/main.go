// Command server runs dahlunas. Cloud Run sets PORT.
package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/ardeman/project-dahlunas-go/internal/app"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	handler, err := app.Handler()
	if err != nil {
		log.Fatal(err)
	}
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      90 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Printf("dahlunas listening on :%s", port)
	log.Fatal(srv.ListenAndServe())
}
