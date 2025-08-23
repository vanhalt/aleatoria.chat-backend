package main

import (
	"net/http"
	"testing"
)

func TestValidateOrigin(t *testing.T) {
	// Restore original allowedOrigins after the test
	originalAllowedOrigins := allowedOrigins
	defer func() {
		allowedOrigins = originalAllowedOrigins
	}()

	allowedOrigins = []string{"https://www.aleatoria.chat", "https://sub.aleatoria.chat"}

	tests := []struct {
		name   string
		origin string
		want   bool
	}{
		{
			name:   "Allowed origin",
			origin: "https://www.aleatoria.chat",
			want:   true,
		},
		{
			name:   "Allowed subdomain",
			origin: "https://sub.aleatoria.chat",
			want:   true,
		},
		{
			name:   "Disallowed origin",
			origin: "https://www.evil.com",
			want:   false,
		},
		{
			name:   "Different scheme",
			origin: "http://www.aleatoria.chat",
			want:   false,
		},
		{
			name:   "No origin header",
			origin: "",
			want:   false,
		},
		{
			name:   "Malformed origin header",
			origin: "not-a-url",
			want:   false,
		},
		{
			name:   "Origin with port",
			origin: "https://www.aleatoria.chat:443",
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, _ := http.NewRequest("GET", "/", nil)
			if tt.origin != "" {
				r.Header.Set("Origin", tt.origin)
			}
			if got := ValidateOrigin(r); got != tt.want {
				t.Errorf("ValidateOrigin() = %v, want %v", got, tt.want)
			}
		})
	}
}
