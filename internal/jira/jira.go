// Package jira fetches read-only ticket info (status, assignee, summary) from
// the Jira REST API for inline display. Auth is Jira Cloud Basic (email + API
// token); the feature is off unless baseURL + email + token are all present.
package jira

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Ticket is the trimmed view of a Jira issue mrglass renders.
type Ticket struct {
	Key            string
	Summary        string
	Status         string // e.g. "In Review"
	StatusCategory string // "new" (To Do) | "indeterminate" (In Progress) | "done"
	Assignee       string // displayName, or "Unassigned"
}

// Client fetches a ticket by key.
type Client interface {
	Fetch(key string) (Ticket, error)
}

// doer is the minimal http.Client surface, injectable for tests.
type doer interface {
	Do(*http.Request) (*http.Response, error)
}

// HTTPClient implements Client against the Jira Cloud REST v3 API.
type HTTPClient struct {
	BaseURL string
	Email   string
	Token   string
	HTTP    doer
}

// Configured reports whether all three credentials are present.
func Configured(baseURL, email, token string) bool {
	return baseURL != "" && email != "" && token != ""
}

// FromEnv reads the Jira credentials from the environment.
func FromEnv() (email, token string) {
	return os.Getenv("JIRA_EMAIL"), os.Getenv("JIRA_API_TOKEN")
}

func (c HTTPClient) Fetch(key string) (Ticket, error) {
	base := strings.TrimRight(c.BaseURL, "/")
	url := fmt.Sprintf("%s/rest/api/3/issue/%s?fields=summary,status,assignee", base, key)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return Ticket{}, err
	}
	req.SetBasicAuth(c.Email, c.Token)
	req.Header.Set("Accept", "application/json")

	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return Ticket{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Cloud often returns 404 for auth failures on issue lookups — treat any
		// non-2xx uniformly as "status unavailable".
		return Ticket{}, fmt.Errorf("jira %s: HTTP %d", key, resp.StatusCode)
	}
	return parseIssue(body)
}

// parseIssue maps a Jira issue JSON payload to a Ticket. A nil assignee becomes
// "Unassigned".
func parseIssue(raw []byte) (Ticket, error) {
	var doc struct {
		Key    string `json:"key"`
		Fields struct {
			Summary string `json:"summary"`
			Status  struct {
				Name           string `json:"name"`
				StatusCategory struct {
					Key string `json:"key"`
				} `json:"statusCategory"`
			} `json:"status"`
			Assignee *struct {
				DisplayName string `json:"displayName"`
			} `json:"assignee"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return Ticket{}, err
	}
	assignee := "Unassigned"
	if doc.Fields.Assignee != nil && doc.Fields.Assignee.DisplayName != "" {
		assignee = doc.Fields.Assignee.DisplayName
	}
	return Ticket{
		Key:            doc.Key,
		Summary:        doc.Fields.Summary,
		Status:         doc.Fields.Status.Name,
		StatusCategory: doc.Fields.Status.StatusCategory.Key,
		Assignee:       assignee,
	}, nil
}
