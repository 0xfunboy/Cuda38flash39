package app

import "testing"

func TestJSONPromptHistoricalDefaultUnchanged(t *testing.T) {
	p := buildCodingPrompt("fix", map[string]string{"a.cpp": "x\ny"}, []string{"a.cpp"}, "")
	if p != "TASK\nfix\nALLOWED PATHS\na.cpp\nSELECTED CURRENT SOURCES\n{\"a.cpp\":\"x\\ny\"}" {
		t.Fatal(p)
	}
}

func TestJSONPromptHistoricalRepairUnchanged(t *testing.T) {
	p := buildCodingPrompt("fix", map[string]string{"a.cpp": "x\ny"}, []string{"a.cpp"}, "compiler error")
	want := "TASK\nfix\nALLOWED PATHS\na.cpp\nSELECTED CURRENT SOURCES\n{\"a.cpp\":\"x\\ny\"}\nRELEVANT FAILURE FROM PREVIOUS ATTEMPT\ncompiler error\nCorrect the current code, provide final JSON. The prior conversation is intentionally omitted."
	if p != want {
		t.Fatalf("historical repair prompt changed:\n%s", p)
	}
}

func TestCodingHonorsSelectedProfileUnlessExplicit(t *testing.T) {
	a := &App{cfg: Config{DefaultProfile: "fast", MaxContextTokens: 50000, Profiles: map[string]Profile{
		"fast":     {Reasoning: "low", ContextTokens: 8192, MaxTokens: 4096, MaxRepairs: 2},
		"balanced": {Reasoning: "high", ContextTokens: 16384, MaxTokens: 8192, MaxRepairs: 2},
	}}}
	a.preferred.Store("balanced")
	p, e := a.resolveProfile(TaskSpec{})
	if e != nil || p.Reasoning != "high" {
		t.Fatalf("selected profile ignored: %+v %v", p, e)
	}
	p, e = a.resolveProfile(TaskSpec{Profile: "fast"})
	if e != nil || p.Reasoning != "low" {
		t.Fatalf("explicit profile overridden: %+v %v", p, e)
	}
}
