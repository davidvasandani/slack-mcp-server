package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/slack-go/slack"
	"github.com/stretchr/testify/require"
)

func TestUnitSearchUsersEmailCapability(t *testing.T) {
	for _, tc := range []struct {
		name, scopes, authError, query, wantError string
		found                                     bool
	}{
		{name: "missing scope name", scopes: "users:read", query: "alice", wantError: "missing_scope: users:read.email"},
		{name: "missing scope email", scopes: "users:read", query: "alice@example.com", wantError: "missing_scope: users:read.email"},
		{name: "missing scope ID", scopes: "users:read", query: "U123", wantError: "missing_scope: users:read.email"},
		{name: "unknown scope", query: "alice", wantError: "email_scope_unverified"},
		{name: "invalid token", authError: "invalid_auth", query: "alice", wantError: "invalid_auth"},
		{name: "exact email bypasses stale cache", scopes: "users:read, users:read.email", query: "alice@example.com", found: true},
		{name: "name refreshes stale profile", scopes: "users:read,users:read.email", query: "alice", found: true},
		{name: "ID refreshes profile", scopes: "users:read,users:read.email", query: "U123", found: true},
		{name: "unknown email", scopes: "users:read,users:read.email", query: "missing@example.com"},
		{name: "empty name result", scopes: "users:read,users:read.email", query: "nobody"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			profileCalls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-OAuth-Scopes", tc.scopes)
				if r.URL.Path == "/auth.test" {
					if tc.authError != "" {
						json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": tc.authError})
					} else {
						w.Write([]byte(`{"ok":true,"user_id":"U123"}`))
					}
					return
				}
				profileCalls++
				if r.URL.Path == "/users.lookupByEmail" && r.FormValue("email") == "missing@example.com" {
					w.Write([]byte(`{"ok":false,"error":"users_not_found"}`))
					return
				}
				require.Contains(t, []string{"/users.info", "/users.lookupByEmail"}, r.URL.Path)
				w.Write([]byte(`{"ok":true,"user":{"id":"U123","name":"alice","profile":{"email":"alice@example.com"}}}`))
			}))
			defer server.Close()
			client := server.Client()
			client.Transport = emailScopeTransport{next: client.Transport}
			ap := &ApiProvider{client: &MCPSlackClient{isOAuth: true, slackClient: slack.New("xoxp-test", slack.OptionAPIURL(server.URL+"/"), slack.OptionHTTPClient(client))}}
			ap.usersReady.Store(true)
			ap.usersSnapshot.Store(&UsersCache{Users: map[string]slack.User{"U123": {ID: "U123", Name: "alice"}}})
			users, err := ap.SearchUsers(context.Background(), tc.query, 10)
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
				require.Zero(t, profileCalls)
				require.Nil(t, users)
				return
			}
			require.NoError(t, err)
			if tc.found {
				require.Len(t, users, 1)
				require.Equal(t, "alice@example.com", users[0].Profile.Email)
			} else {
				require.Empty(t, users)
			}
		})
	}
}

func TestUnitEmailScopeErrorDoesNotExposeToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-OAuth-Scopes", "users:read")
		w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	client := server.Client()
	client.Transport = emailScopeTransport{next: client.Transport}
	c := &MCPSlackClient{slackClient: slack.New("xoxp-secret-fixture", slack.OptionAPIURL(server.URL+"/"), slack.OptionHTTPClient(client))}
	err := c.requireEmailScope(context.Background())
	require.Error(t, err)
	require.False(t, strings.Contains(err.Error(), "xoxp-secret-fixture"))
}
