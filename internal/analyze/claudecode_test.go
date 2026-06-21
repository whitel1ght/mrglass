package analyze

import (
	"errors"
	"strings"
	"testing"

	"github.com/dmitry/mrglass/internal/core"
)

type fakeCmd struct {
	out  []byte
	err  error
	args []string
	in   string
}

func (f *fakeCmd) Run(stdin string, args ...string) ([]byte, error) {
	f.in = stdin
	f.args = args
	return f.out, f.err
}

func TestParseResult(t *testing.T) {
	out := []byte(`{"type":"result","result":"CI failed on lint; run make fmt.","is_error":false}`)
	got, err := parseResult(out)
	if err != nil {
		t.Fatal(err)
	}
	if got != "CI failed on lint; run make fmt." {
		t.Errorf("got %q", got)
	}
}

func TestTriageHappyPath(t *testing.T) {
	f := &fakeCmd{out: []byte(`{"result":"rebase onto master"}`)}
	cc := ClaudeCode{R: f}
	c := core.Change{Ref: "g/p!1", Detail: "conflicts appeared",
		Fields: []core.FieldChange{{Field: "conflicts", Old: false, New: true}}}
	adv := cc.Triage(c)
	if adv.Err != nil {
		t.Fatalf("unexpected err: %v", adv.Err)
	}
	if adv.Ref != "g/p!1" || adv.Text != "rebase onto master" {
		t.Errorf("advice = %+v", adv)
	}
	// uses read-only, headless, json flags
	joined := strings.Join(f.args, " ")
	for _, want := range []string{"-p", "--output-format", "json", "--allowedTools", "--bare"} {
		if !strings.Contains(joined, want) {
			t.Errorf("args %q missing %q", joined, want)
		}
	}
}

func TestParseResultEmptyResultErrors(t *testing.T) {
	_, err := parseResult([]byte("{}"))
	if err == nil {
		t.Error("expected error for empty result, got nil")
	}
}

func TestParseResultMalformed(t *testing.T) {
	_, err := parseResult([]byte("not json"))
	if err == nil {
		t.Error("expected error for malformed JSON, got nil")
	}
}

func TestTriageSubprocessError(t *testing.T) {
	f := &fakeCmd{err: errors.New("claude exploded")}
	cc := ClaudeCode{R: f}
	adv := cc.Triage(core.Change{Ref: "g/p!1"})
	if adv.Err == nil {
		t.Error("subprocess error should surface in Advice.Err")
	}
}
