package jira

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"testing"
)

const issueJSON = `{
  "key": "ECFX-1234",
  "fields": {
    "summary": "Inject PROCESSOR_LOGGING_BUCKET",
    "status": { "name": "In Review", "statusCategory": { "key": "indeterminate" } },
    "assignee": { "displayName": "Jane Smith" }
  }
}`

func TestParseIssue(t *testing.T) {
	tk, err := parseIssue([]byte(issueJSON))
	if err != nil {
		t.Fatal(err)
	}
	if tk.Key != "ECFX-1234" || tk.Summary != "Inject PROCESSOR_LOGGING_BUCKET" {
		t.Errorf("key/summary wrong: %+v", tk)
	}
	if tk.Status != "In Review" || tk.StatusCategory != "indeterminate" {
		t.Errorf("status wrong: %+v", tk)
	}
	if tk.Assignee != "Jane Smith" {
		t.Errorf("assignee = %q", tk.Assignee)
	}
}

func TestParseIssueNullAssignee(t *testing.T) {
	js := `{"key":"X-1","fields":{"summary":"s","status":{"name":"To Do","statusCategory":{"key":"new"}},"assignee":null}}`
	tk, err := parseIssue([]byte(js))
	if err != nil {
		t.Fatal(err)
	}
	if tk.Assignee != "Unassigned" {
		t.Errorf("null assignee should be Unassigned, got %q", tk.Assignee)
	}
}

type fakeDoer struct {
	status int
	body   string
	err    error
	gotReq *http.Request
}

func (f *fakeDoer) Do(req *http.Request) (*http.Response, error) {
	f.gotReq = req
	if f.err != nil {
		return nil, f.err
	}
	return &http.Response{
		StatusCode: f.status,
		Body:       io.NopCloser(bytes.NewBufferString(f.body)),
	}, nil
}

func TestFetchOK(t *testing.T) {
	f := &fakeDoer{status: 200, body: issueJSON}
	c := HTTPClient{BaseURL: "https://ecfx.atlassian.net/", Email: "e@x.com", Token: "tok", HTTP: f}
	tk, err := c.Fetch("ECFX-1234")
	if err != nil {
		t.Fatal(err)
	}
	if tk.Status != "In Review" {
		t.Errorf("status = %q", tk.Status)
	}
	// correct URL (trailing slash trimmed) + Basic auth + Accept
	if got := f.gotReq.URL.String(); got != "https://ecfx.atlassian.net/rest/api/3/issue/ECFX-1234?fields=summary,status,assignee" {
		t.Errorf("url = %q", got)
	}
	if u, p, ok := f.gotReq.BasicAuth(); !ok || u != "e@x.com" || p != "tok" {
		t.Errorf("basic auth not set correctly: %q/%q ok=%v", u, p, ok)
	}
}

func TestFetchNon2xxIsError(t *testing.T) {
	f := &fakeDoer{status: 404, body: `{"errorMessages":["nope"]}`}
	c := HTTPClient{BaseURL: "https://x", Email: "e", Token: "t", HTTP: f}
	if _, err := c.Fetch("ECFX-1"); err == nil {
		t.Error("non-2xx must be an error (Cloud returns 404 for auth failures)")
	}
}

func TestFetchTransportError(t *testing.T) {
	f := &fakeDoer{err: errors.New("dial fail")}
	c := HTTPClient{BaseURL: "https://x", Email: "e", Token: "t", HTTP: f}
	if _, err := c.Fetch("ECFX-1"); err == nil {
		t.Error("transport error should surface")
	}
}

func TestConfigured(t *testing.T) {
	if !Configured("https://x", "e", "t") {
		t.Error("all present should be configured")
	}
	for _, c := range [][3]string{{"", "e", "t"}, {"x", "", "t"}, {"x", "e", ""}} {
		if Configured(c[0], c[1], c[2]) {
			t.Errorf("missing field should be unconfigured: %v", c)
		}
	}
}
