package review

import (
	"errors"
	"strings"
	"testing"

	"github.com/dmitry/mrglass/internal/core"
)

type fakeGitLab struct {
	diff       string
	diffErr    error
	posted     string
	postErr    error
	postCalled bool
}

func (f *fakeGitLab) MRDiff(int, int) (string, error) { return f.diff, f.diffErr }
func (f *fakeGitLab) PostNote(_, _ int, body string) error {
	f.postCalled = true
	f.posted = body
	return f.postErr
}

type fakeReviewer struct {
	gotDiff   string
	gotPrompt string
	result    Result
}

func (f *fakeReviewer) Review(_ core.MR, diff, prompt string) Result {
	f.gotDiff, f.gotPrompt = diff, prompt
	return f.result
}

func mr() core.MR { return core.MR{Ref: "g/p!1", IID: 1, ProjectID: 9, Title: "t"} }

func TestGenerateFeedsDiffAndPrompt(t *testing.T) {
	gl := &fakeGitLab{diff: "some diff"}
	rv := &fakeReviewer{result: Result{Ref: "g/p!1", Text: "looks good"}}
	res := Generate(gl, rv, mr(), "REVIEW PROMPT")
	if res.Err != nil {
		t.Fatalf("err: %v", res.Err)
	}
	if rv.gotDiff != "some diff" || rv.gotPrompt != "REVIEW PROMPT" {
		t.Errorf("reviewer got diff=%q prompt=%q", rv.gotDiff, rv.gotPrompt)
	}
	if res.Text != "looks good" {
		t.Errorf("text = %q", res.Text)
	}
}

func TestGenerateDiffErrorSurfaces(t *testing.T) {
	gl := &fakeGitLab{diffErr: errors.New("boom")}
	rv := &fakeReviewer{}
	if res := Generate(gl, rv, mr(), "p"); res.Err == nil {
		t.Error("diff error should surface")
	}
}

func TestGenerateEmptyDiffIsError(t *testing.T) {
	gl := &fakeGitLab{diff: ""}
	if res := Generate(gl, &fakeReviewer{}, mr(), "p"); res.Err == nil {
		t.Error("empty diff should be an error, not a review")
	}
}

func TestGenerateTruncatesHugeDiff(t *testing.T) {
	gl := &fakeGitLab{diff: strings.Repeat("x", maxDiffChars+5000)}
	rv := &fakeReviewer{result: Result{Text: "ok"}}
	Generate(gl, rv, mr(), "p")
	if len(rv.gotDiff) > maxDiffChars+50 {
		t.Errorf("diff should be truncated, got %d chars", len(rv.gotDiff))
	}
	if !strings.Contains(rv.gotDiff, "truncated") {
		t.Error("truncated diff should be marked")
	}
}

func TestPostWritesNote(t *testing.T) {
	gl := &fakeGitLab{}
	if err := Post(gl, mr(), "the review"); err != nil {
		t.Fatalf("err: %v", err)
	}
	if !gl.postCalled || gl.posted != "the review" {
		t.Errorf("post not performed correctly: called=%v body=%q", gl.postCalled, gl.posted)
	}
}

// --- ClaudeReviewer invocation ---

type fakeCmd struct {
	out  []byte
	err  error
	args []string
	in   string
}

func (f *fakeCmd) Run(stdin string, args ...string) ([]byte, error) {
	f.in, f.args = stdin, args
	return f.out, f.err
}

func TestClaudeReviewerReadOnlyFlags(t *testing.T) {
	f := &fakeCmd{out: []byte(`{"result":"a review"}`)}
	cr := ClaudeReviewer{R: f}
	res := cr.Review(mr(), "diff here", "be concise")
	if res.Err != nil {
		t.Fatalf("err: %v", res.Err)
	}
	if res.Text != "a review" {
		t.Errorf("text = %q", res.Text)
	}
	joined := strings.Join(f.args, " ")
	for _, want := range []string{"-p", "--output-format", "json", "--allowedTools", "--bare"} {
		if !strings.Contains(joined, want) {
			t.Errorf("args missing %q: %s", want, joined)
		}
	}
	// READ-ONLY: must not grant Edit/Write/Bash
	for _, banned := range []string{"Edit", "Write", "Bash"} {
		if strings.Contains(joined, banned) {
			t.Errorf("review must be read-only, but args grant %q: %s", banned, joined)
		}
	}
	// the prompt and diff must both reach claude
	if !strings.Contains(f.in, "be concise") || !strings.Contains(f.in, "diff here") {
		t.Errorf("stdin should carry prompt+diff: %q", f.in)
	}
}

func TestClaudeReviewerSubprocessError(t *testing.T) {
	f := &fakeCmd{err: errors.New("claude died")}
	if res := (ClaudeReviewer{R: f}).Review(mr(), "d", "p"); res.Err == nil {
		t.Error("subprocess error should surface")
	}
}

func TestClaudeReviewerIsErrorNotTreatedAsReview(t *testing.T) {
	// claude can exit 0 but report is_error:true (e.g. "Not logged in"). That
	// message must NOT become a review (it would otherwise be posted to the MR).
	f := &fakeCmd{out: []byte(`{"type":"result","is_error":true,"result":"Not logged in · Please run /login"}`)}
	res := (ClaudeReviewer{R: f}).Review(mr(), "d", "p")
	if res.Err == nil {
		t.Fatal("is_error result must surface as an error, not a review")
	}
	if res.Text != "" {
		t.Errorf("no review text should be returned on error, got %q", res.Text)
	}
	if !strings.Contains(res.Err.Error(), "Not logged in") {
		t.Errorf("error should carry claude's message, got %v", res.Err)
	}
}
