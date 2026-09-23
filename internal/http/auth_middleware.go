package http

import (
	"context"
	"crypto/subtle"
	stdhttp "net/http"
	"strings"

	portyauth "github.com/msoldin/porty/internal/auth"
)

type principalKey struct{}

func principalFrom(ctx context.Context) portyauth.Principal {
	principal, _ := ctx.Value(principalKey{}).(portyauth.Principal)
	return principal
}

func authenticatedRoute(options RouterOptions, next stdhttp.HandlerFunc) stdhttp.HandlerFunc {
	return func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		_, principal, ok := authenticate(w, r, options.Auth)
		if !ok {
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, principal)))
	}
}

func mutationAuthRoute(options RouterOptions, next stdhttp.HandlerFunc) stdhttp.HandlerFunc {
	return func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		_, principal, ok := requireMutationAuth(w, r, options)
		if !ok {
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, principal)))
	}
}

func authenticate(w stdhttp.ResponseWriter, r *stdhttp.Request, service *portyauth.AuthService) (string, portyauth.Principal, bool) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		WriteError(w, r, stdhttp.StatusUnauthorized, "AuthenticationFailed", "Authentication required", nil)
		return "", portyauth.Principal{}, false
	}
	principal, err := service.Authenticate(r.Context(), cookie.Value)
	if err != nil {
		WriteError(w, r, stdhttp.StatusUnauthorized, "AuthenticationFailed", "Authentication required", nil)
		return "", portyauth.Principal{}, false
	}
	return cookie.Value, principal, true
}

func requireMutationAuth(w stdhttp.ResponseWriter, r *stdhttp.Request, options RouterOptions) (string, portyauth.Principal, bool) {
	if !validOrigin(r, options.PublicURL) {
		WriteError(w, r, stdhttp.StatusForbidden, "PermissionDenied", "Request origin is invalid", nil)
		return "", portyauth.Principal{}, false
	}
	raw, principal, ok := authenticate(w, r, options.Auth)
	if !ok {
		return "", portyauth.Principal{}, false
	}
	if !validDoubleSubmit(r, csrfCookieName) || !options.Auth.CheckCSRF(principal, r.Header.Get("X-CSRF-Token")) {
		WriteError(w, r, stdhttp.StatusForbidden, "PermissionDenied", "CSRF token is invalid", nil)
		return "", portyauth.Principal{}, false
	}
	return raw, principal, true
}

func validOrigin(r *stdhttp.Request, publicURL string) bool {
	expected := strings.TrimRight(publicURL, "/")
	if expected == "" {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		expected = scheme + "://" + r.Host
	}
	return r.Header.Get("Origin") == expected
}

func validDoubleSubmit(r *stdhttp.Request, name string) bool {
	cookie, err := r.Cookie(name)
	if err != nil {
		return false
	}
	header := r.Header.Get("X-CSRF-Token")
	return len(cookie.Value) == len(header) && subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(header)) == 1
}
