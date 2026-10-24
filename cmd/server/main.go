// Command server runs dahlunas. Cloud Run sets PORT.
package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/ardeman/project-dahlunas-go/internal/web"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           web.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("dahlunas listening on :%s", port)
	log.Fatal(srv.ListenAndServe())
}
