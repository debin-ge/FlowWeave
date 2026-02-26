package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPublicCreateRoutesBypassJWT(t *testing.T) {
	cfg := DefaultServerConfig()
	cfg.JWTSecret = "test-secret"
	server := NewServer(cfg, nil, nil)
	handler := server.Handler()

	tests := []struct {
		name string
		path string
		body string
	}{
		{
			name: "create organization without token",
			path: "/organizations",
			body: `{}`,
		},
		{
			name: "create tenant without token",
			path: "/tenants",
			body: `{}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			if rr.Code == http.StatusUnauthorized {
				t.Fatalf("expected public route %s to bypass JWT, got 401", tt.path)
			}
		})
	}
}

func TestProtectedRoutesStillRequireJWT(t *testing.T) {
	cfg := DefaultServerConfig()
	cfg.JWTSecret = "test-secret"
	server := NewServer(cfg, nil, nil)
	handler := server.Handler()

	tests := []struct {
		name string
		path string
	}{
		{
			name: "list organizations requires jwt",
			path: "/organizations",
		},
		{
			name: "list tenants requires jwt",
			path: "/tenants",
		},
		{
			name: "list workflows requires jwt",
			path: "/api/v1/workflows",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			if rr.Code != http.StatusUnauthorized {
				t.Fatalf("expected 401 for protected route %s, got %d", tt.path, rr.Code)
			}
		})
	}
}
