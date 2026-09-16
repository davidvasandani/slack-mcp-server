package provider

import (
	"context"
	"errors"
	"net/http"
	"strings"
)

// Capture scopes only for this request: no shared mutable capability cache.
type emailScopeCheckKey struct{}
type emailScopeTransport struct{ next http.RoundTripper }

func (t emailScopeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	next := t.next
	if next == nil {
		next = http.DefaultTransport
	}
	resp, err := next.RoundTrip(req)
	if err == nil {
		if scopes, ok := req.Context().Value(emailScopeCheckKey{}).(*string); ok {
			*scopes = resp.Header.Get("X-OAuth-Scopes")
		}
	}
	return resp, err
}

func (c *MCPSlackClient) requireEmailScope(ctx context.Context) error {
	var scopes string
	if _, err := c.slackClient.AuthTestContext(context.WithValue(ctx, emailScopeCheckKey{}, &scopes)); err != nil {
		return err
	}
	if scopes == "" {
		return errors.New("email_scope_unverified: Slack did not report token scopes; retry or check the OAuth installation")
	}
	for _, scope := range strings.Split(scopes, ",") {
		if strings.TrimSpace(scope) == "users:read.email" {
			return nil
		}
	}
	return errors.New("missing_scope: users:read.email; add the user/bot token scope, reinstall the Slack app, and replace the server token")
}
