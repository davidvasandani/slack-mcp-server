package provider

import (
	"context"
	"errors"
	"github.com/korotovsky/slack-mcp-server/pkg/limiter"
	"github.com/slack-go/slack"
	"net/http"
	"strings"
	"time"
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

// A bounded TTL avoids repeating successful profile reads after a later 429.
// The map is keyed by workspace user ID and is private to this token's client.
type emailProfile struct {
	user slack.User
	at   time.Time
}

func (c *MCPSlackClient) emailProfile(ctx context.Context, id string) (*slack.User, error) {
	if cached, ok := c.emailProfiles.Load(id); ok {
		entry := cached.(emailProfile)
		if time.Since(entry.at) < 5*time.Minute {
			user := entry.user
			return &user, nil
		}
	}
	c.emailLimiterOnce.Do(func() { c.emailLimiter = limiter.Tier3.Limiter() })
	user, err := limiter.CallWithRetry(ctx, c.emailLimiter, 2, func(err error) time.Duration {
		var limited *slack.RateLimitedError
		if errors.As(err, &limited) {
			return limited.RetryAfter
		}
		return 0
	}, func() (*slack.User, error) { return c.slackClient.GetUserInfoContext(ctx, id) })
	if err == nil {
		c.emailProfiles.Store(id, emailProfile{user: *user, at: time.Now()})
	}
	return user, err
}
