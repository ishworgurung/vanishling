package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
)

type Vanishable interface {
	download(w http.ResponseWriter, r *http.Request)
	upload(w http.ResponseWriter, r *http.Request)
	delete(w http.ResponseWriter, r *http.Request)
}

func main() {

	// add route / POST
	// o if no ttl provided use default from config or else use the provided ttl
	// o upload the file and store it in filesystem
	// o after the ttl expire, delete the file from fs
	// o return auth key

	// add route / GET
	// o if the auth key correct, fetch the file
	// o if the auth key incorrect, throw 4xxs

	fileUploaderService, err := newFileUploaderSvc()
	if err != nil {
		log.Fatal(err)
	}
	healthCheckService, err := newHealthCheckSvc()
	if err != nil {
		log.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("/ping", healthCheckService)
	mux.Handle("/", fileUploaderService)

	listenPort := os.Getenv("PORT")
	if len(listenPort) == 0 {
		listenPort = "8080"
	}
	log.Printf("listening on :%s\n", listenPort)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%s", listenPort), mux))

}
