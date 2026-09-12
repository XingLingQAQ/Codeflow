package api

import "net/http"

const testAuthToken = "codeflow-test-token-32-characters-long"

func authenticatedTestHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			r.Header.Set("Authorization", "Bearer "+testAuthToken)
		}
		next.ServeHTTP(w, r)
	})
}

type authenticatedTestServer struct {
	server *Server
}

func (s *authenticatedTestServer) Router() http.Handler {
	return authenticatedTestHandler(s.server.Router())
}
