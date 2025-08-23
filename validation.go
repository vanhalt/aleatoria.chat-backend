package main

import (
	"log"
	"net/http"
	"net/url"
)

// allowedOrigins is a list of trusted origins.
// For production, this should be loaded from a configuration file or environment variable.
var allowedOrigins = []string{"https://www.aleatoria.chat"}

// ValidateOrigin checks if the origin of the request is allowed.
func ValidateOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		log.Println("Rejecting connection with no Origin header")
		return false
	}

	u, err := url.Parse(origin)
	if err != nil {
		log.Printf("Rejecting connection with malformed Origin header: %s", origin)
		return false
	}

	for _, allowedOrigin := range allowedOrigins {
		allowedURL, err := url.Parse(allowedOrigin)
		if err != nil {
			log.Printf("Invalid allowed origin configured: %s", allowedOrigin)
			continue
		}

		// Compare scheme and hostname
		if u.Scheme == allowedURL.Scheme && u.Hostname() == allowedURL.Hostname() {
			uPort := u.Port()
			allowedPort := allowedURL.Port()

			// Normalize ports for comparison
			if uPort == "" {
				if u.Scheme == "https" {
					uPort = "443"
				} else {
					uPort = "80"
				}
			}
			if allowedPort == "" {
				if allowedURL.Scheme == "https" {
					allowedPort = "443"
				} else {
					allowedPort = "80"
				}
			}

			if uPort == allowedPort {
				log.Printf("Accepting connection from origin: %s", origin)
				return true
			}
		}
	}

	log.Printf("Rejecting connection from origin: %s", origin)
	return false
}
