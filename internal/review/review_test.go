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
	gotDir    string
	gotSkill  string
	result    Result
}

func (f *fakeReviewer) Review(req ReviewReq) Result {
	f.gotDiff, f.gotPrompt, f.gotDir, f.gotSkill = req.Diff, req.Prompt, req.Dir, req.Skill
	return f.result
}

func mr() core.MR { return core.MR{Ref: "g/p!1", IID: 1, ProjectID: 9, Title: "t"} }

func TestGenerateFeedsDiffAndPrompt(t *testing.T) {
	gl := &fakeGitLab{diff: "some diff"}
	rv := &fakeReviewer{result: Result{Ref: "g/p!1", Text: "looks good"}}
	res := Generate(gl, rv, mr(), "REVIEW PROMPT", Options{})
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
	if res := Generate(gl, rv, mr(), "p", Options{}); res.Err == nil {
		t.Error("diff error should surface")
	}
}

func TestGenerateEmptyDiffIsError(t *testing.T) {
	gl := &fakeGitLab{diff: ""}
	if res := Generate(gl, &fakeReviewer{}, mr(), "p", Options{}); res.Err == nil {
		t.Error("empty diff should be an error, not a review")
	}
}

func TestGenerateTruncatesHugeDiff(t *testing.T) {
	gl := &fakeGitLab{diff: strings.Repeat("x", maxDiffChars+5000)}
	rv := &fakeReviewer{result: Result{Text: "ok"}}
	Generate(gl, rv, mr(), "p", Options{})
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

func (f *fakeCmd) Run(stdin, dir string, args ...string) ([]byte, error) {
	f.in, f.args = stdin, args
	return f.out, f.err
}

func TestClaudeReviewerReadOnlyFlags(t *testing.T) {
	f := &fakeCmd{out: []byte(`{"result":"a review"}`)}
	cr := ClaudeReviewer{R: f}
	res := cr.Review(ReviewReq{MR: mr(), Diff: "diff here", Prompt: "be concise"})
	if res.Err != nil {
		t.Fatalf("err: %v", res.Err)
	}
	if res.Text != "a review" {
		t.Errorf("text = %q", res.Text)
	}
	joined := strings.Join(f.args, " ")
	for _, want := range []string{"-p", "--output-format", "json", "--allowedTools"} {
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
	if res := (ClaudeReviewer{R: f}).Review(ReviewReq{MR: mr(), Diff: "d", Prompt: "p"}); res.Err == nil {
		t.Error("subprocess error should surface")
	}
}

func TestClaudeReviewerStdoutWinsOverExitCode(t *testing.T) {
	// claude exits 1 but still prints a JSON is_error to stdout. The useful
	// message must surface, not the bare "exit status 1".
	f := &fakeCmd{
		out: []byte(`{"is_error":true,"result":"Not logged in · Please run /login"}`),
		err: errors.New("exit status 1"),
	}
	res := (ClaudeReviewer{R: f}).Review(ReviewReq{MR: mr(), Diff: "d", Prompt: "p"})
	if res.Err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(res.Err.Error(), "Not logged in") {
		t.Errorf("is_error message should win over exit code, got: %v", res.Err)
	}
}

func TestClaudeReviewerExitErrorWhenNoStdout(t *testing.T) {
	// Non-zero exit with no parseable stdout -> the process error surfaces.
	f := &fakeCmd{out: []byte(""), err: errors.New("exit status 1: some stderr")}
	res := (ClaudeReviewer{R: f}).Review(ReviewReq{MR: mr(), Diff: "d", Prompt: "p"})
	if res.Err == nil || !strings.Contains(res.Err.Error(), "stderr") {
		t.Errorf("process error (with stderr) should surface, got: %v", res.Err)
	}
}

func TestClaudeReviewerWithSkillUsesStreamAndReports(t *testing.T) {
	stream := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Skill","input":{"skill":"superpowers:requesting-code-review"}}]}}
{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Task","input":{"description":"x"}}]}}
{"type":"result","is_error":false,"result":"## Review\nok"}`
	f := &fakeCmd{out: []byte(stream)}
	res := (ClaudeReviewer{R: f}).Review(ReviewReq{
		MR: mr(), Diff: "d", Prompt: "p", Skill: "superpowers:requesting-code-review",
	})
	if res.Err != nil {
		t.Fatalf("err: %v", res.Err)
	}
	if res.Text != "## Review\nok" {
		t.Errorf("text = %q", res.Text)
	}
	if len(res.SkillsUsed) != 1 || res.SkillsUsed[0] != "superpowers:requesting-code-review" {
		t.Errorf("SkillsUsed = %v (should report the invoked skill)", res.SkillsUsed)
	}
	if res.Subagents != 1 {
		t.Errorf("Subagents = %d, want 1", res.Subagents)
	}
	// must use stream-json and grant the skill/subagent tools, but NO writes
	joined := strings.Join(f.args, " ")
	for _, want := range []string{"stream-json", "--verbose", "Skill", "Task"} {
		if !strings.Contains(joined, want) {
			t.Errorf("skill review args missing %q: %s", want, joined)
		}
	}
	// the prompt instructs claude to invoke the skill
	if !strings.Contains(f.in, "Skill tool") || !strings.Contains(f.in, "superpowers:requesting-code-review") {
		t.Errorf("prompt should instruct invoking the skill: %q", f.in)
	}
	// draft-only guard: the skill must not post / ask to post itself
	if !strings.Contains(f.in, "Do NOT post") {
		t.Errorf("skill prompt should carry the draft-only guard: %q", f.in)
	}
}

func TestClaudeReviewerSkillPassesPluginDirs(t *testing.T) {
	stream := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Skill","input":{"skill":"claude-components:mr-review"}}]}}
{"type":"result","is_error":false,"result":"ok"}`
	f := &fakeCmd{out: []byte(stream)}
	(ClaudeReviewer{R: f}).Review(ReviewReq{
		MR: mr(), Diff: "d", Prompt: "p",
		Skill:      "claude-components:mr-review",
		PluginDirs: []string{"/abs/claude-components", "~/projects/claude-components"},
	})
	joined := strings.Join(f.args, " ")
	if !strings.Contains(joined, "--plugin-dir /abs/claude-components") {
		t.Errorf("absolute plugin dir not passed: %s", joined)
	}
	// the ~ form must be expanded (no literal "~/" in the passed arg)
	if strings.Contains(joined, "--plugin-dir ~/") {
		t.Errorf("plugin dir ~ should be expanded: %s", joined)
	}
	if !strings.Contains(joined, "projects/claude-components") {
		t.Errorf("expanded plugin dir missing: %s", joined)
	}
}

func TestClaudeReviewerPlainReviewIgnoresPluginDirs(t *testing.T) {
	// No skill -> plain json path -> no --plugin-dir (they're skill-only).
	f := &fakeCmd{out: []byte(`{"result":"ok"}`)}
	(ClaudeReviewer{R: f}).Review(ReviewReq{
		MR: mr(), Diff: "d", Prompt: "p", PluginDirs: []string{"/x"},
	})
	if strings.Contains(strings.Join(f.args, " "), "--plugin-dir") {
		t.Error("plain review must not pass --plugin-dir")
	}
}

func TestClaudeReviewerSkillNotInvokedStillReturnsText(t *testing.T) {
	// Configured a skill, but claude never called the Skill tool. We still get
	// the review text; SkillsUsed is empty so the caller can warn.
	stream := `{"type":"assistant","message":{"content":[{"type":"text","text":"thinking"}]}}
{"type":"result","is_error":false,"result":"a review without the skill"}`
	f := &fakeCmd{out: []byte(stream)}
	res := (ClaudeReviewer{R: f}).Review(ReviewReq{MR: mr(), Diff: "d", Prompt: "p", Skill: "some:skill"})
	if res.Err != nil {
		t.Fatalf("err: %v", res.Err)
	}
	if len(res.SkillsUsed) != 0 {
		t.Errorf("no skill should be reported, got %v", res.SkillsUsed)
	}
	if res.Text == "" {
		t.Error("should still return the review text")
	}
}

func TestClaudeReviewerIsErrorNotTreatedAsReview(t *testing.T) {
	// claude can exit 0 but report is_error:true (e.g. "Not logged in"). That
	// message must NOT become a review (it would otherwise be posted to the MR).
	f := &fakeCmd{out: []byte(`{"type":"result","is_error":true,"result":"Not logged in · Please run /login"}`)}
	res := (ClaudeReviewer{R: f}).Review(ReviewReq{MR: mr(), Diff: "d", Prompt: "p"})
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

func TestGenerateLogsSkillUsed(t *testing.T) {
	var lines []string
	orig := logSink
	logSink = func(l string) { lines = append(lines, l) }
	defer func() { logSink = orig }()

	gl := &fakeGitLab{diff: "d"}
	rv := &fakeReviewer{result: Result{Ref: "g/p!1", Text: "ok",
		SkillsUsed: []string{"claude-components:mr-review"}, Subagents: 3}}
	Generate(gl, rv, mr(), "p", Options{Skill: "claude-components:mr-review"})

	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "OK") || !strings.Contains(joined, "claude-components:mr-review") {
		t.Errorf("success log should record the skill used: %q", joined)
	}
	if !strings.Contains(joined, "subagents=3") {
		t.Errorf("log should record subagent count: %q", joined)
	}
}

func TestGenerateLogsSkillNotInvoked(t *testing.T) {
	var lines []string
	orig := logSink
	logSink = func(l string) { lines = append(lines, l) }
	defer func() { logSink = orig }()

	gl := &fakeGitLab{diff: "d"}
	// configured a skill, but reviewer reports none used
	rv := &fakeReviewer{result: Result{Ref: "g/p!1", Text: "ok"}}
	Generate(gl, rv, mr(), "p", Options{Skill: "some:skill"})

	if !strings.Contains(strings.Join(lines, "\n"), "NOT INVOKED") {
		t.Errorf("log should flag a configured-but-unused skill: %q", lines)
	}
}
