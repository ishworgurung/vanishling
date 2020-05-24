package main

import "net/http"

type healthCheck struct{}

func (p healthCheck) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func newHealthCheckSvc() (*healthCheck, error) {
	return &healthCheck{}, nil
}
