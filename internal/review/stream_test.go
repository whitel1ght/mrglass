package review

import "testing"

func TestParseStreamSkillAndSubagents(t *testing.T) {
	stream := `{"type":"system","subtype":"hook_started"}
{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Skill","input":{"skill":"superpowers:requesting-code-review"}}]}}
{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Task","input":{"description":"review bugs"}}]}}
{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Task","input":{"description":"review perf"}}]}}
{"type":"result","is_error":false,"result":"## Review\nLooks good."}`
	out, err := parseStream([]byte(stream))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(out.SkillsUsed) != 1 || out.SkillsUsed[0] != "superpowers:requesting-code-review" {
		t.Errorf("SkillsUsed = %v", out.SkillsUsed)
	}
	if out.Subagents != 2 {
		t.Errorf("Subagents = %d, want 2", out.Subagents)
	}
	if out.Text != "## Review\nLooks good." {
		t.Errorf("Text = %q", out.Text)
	}
	if out.IsError {
		t.Error("should not be error")
	}
}

func TestParseStreamNoSkill(t *testing.T) {
	stream := `{"type":"assistant","message":{"content":[{"type":"text","text":"thinking"}]}}
{"type":"result","is_error":false,"result":"a plain review"}`
	out, err := parseStream([]byte(stream))
	if err != nil {
		t.Fatal(err)
	}
	if len(out.SkillsUsed) != 0 {
		t.Errorf("expected no skills, got %v", out.SkillsUsed)
	}
	if out.Text != "a plain review" {
		t.Errorf("Text = %q", out.Text)
	}
}

func TestParseStreamError(t *testing.T) {
	stream := `{"type":"result","is_error":true,"result":"Not logged in"}`
	out, err := parseStream([]byte(stream))
	if err != nil {
		t.Fatal(err)
	}
	if !out.IsError || out.ErrMsg != "Not logged in" {
		t.Errorf("error not captured: %+v", out)
	}
}

func TestParseStreamNoResultIsError(t *testing.T) {
	stream := `{"type":"assistant","message":{"content":[]}}`
	if _, err := parseStream([]byte(stream)); err == nil {
		t.Error("a stream with no result event should error")
	}
}

func TestParseStreamSkipsGarbageLines(t *testing.T) {
	stream := "not json at all\n{\"type\":\"result\",\"is_error\":false,\"result\":\"ok\"}\nmore garbage"
	out, err := parseStream([]byte(stream))
	if err != nil || out.Text != "ok" {
		t.Errorf("should skip garbage and find result: out=%+v err=%v", out, err)
	}
}
