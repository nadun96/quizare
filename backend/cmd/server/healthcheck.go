package main

import (
	"net"
	"net/http"
	"os"
	"time"
)

// healthURL turns the listen address into a URL the process itself can call:
// a wildcard host (":8080", "0.0.0.0:8080", "[::]:8080") becomes loopback.
func healthURL(listen string) string {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		host, port = "127.0.0.1", "8080"
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/healthz"
}

// healthcheck backs the container HEALTHCHECK: the runtime image has no
// shell or curl, so the binary checks itself. Exit status 0 means healthy.
func healthcheck() int {
	listen := os.Getenv("QP_LISTEN")
	if listen == "" {
		listen = "127.0.0.1:8080"
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(healthURL(listen))
	if err != nil {
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}
