package studio

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const sessionCookieName = "agentworks_session"

type sessionGuard struct {
	sessionToken string
	csrfToken    string
}

func newSessionGuard() (*sessionGuard, error) {
	sessionToken, err := secureToken()
	if err != nil {
		return nil, fmt.Errorf("create Studio session token: %w", err)
	}
	csrfToken, err := secureToken()
	if err != nil {
		return nil, fmt.Errorf("create Studio CSRF token: %w", err)
	}
	return &sessionGuard{sessionToken: sessionToken, csrfToken: csrfToken}, nil
}

func secureToken() (string, error) {
	data := make([]byte, 32)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func (guard *sessionGuard) issue(writer http.ResponseWriter, _ *http.Request) {
	http.SetCookie(writer, &http.Cookie{
		Name: sessionCookieName, Value: guard.sessionToken, Path: "/",
		HttpOnly: true, SameSite: http.SameSiteStrictMode,
	})
	writer.Header().Set("Cache-Control", "no-store")
	writeJSON(writer, http.StatusOK, map[string]string{"csrf_token": guard.csrfToken})
}

func (guard *sessionGuard) protect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !sameOriginRequest(request) {
			writeJSON(writer, http.StatusForbidden, map[string]string{"error": "request origin is not Studio"})
			return
		}
		cookie, err := request.Cookie(sessionCookieName)
		if err != nil || !constantEqual(cookie.Value, guard.sessionToken) ||
			!constantEqual(request.Header.Get("X-AgentWorks-CSRF"), guard.csrfToken) {
			writeJSON(writer, http.StatusForbidden, map[string]string{"error": "invalid Studio session or CSRF token"})
			return
		}
		next.ServeHTTP(writer, request)
	})
}

func constantEqual(left, right string) bool {
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

func sameOriginRequest(request *http.Request) bool {
	origin := strings.TrimSpace(request.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil {
		return false
	}
	originHost := parsed.Hostname()
	if !isLoopback(originHost) {
		return false
	}
	wantScheme := "http"
	if request.TLS != nil {
		wantScheme = "https"
	}
	return parsed.Scheme == wantScheme && strings.EqualFold(parsed.Host, request.Host)
}
