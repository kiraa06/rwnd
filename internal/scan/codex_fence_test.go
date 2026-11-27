package scan

import "testing"

func TestCodexUnclosedFenceInsideMachineWrapper(t *testing.T) {
	want := "Keep the real prompt after the shell output"
	prompt := "<user_shell_command>\nExit code: 0\n```\nshell output\n</user_shell_command>\n" + want
	got := codexTestScan(t, codexTestMeta(nil)+codexTestMessage("user", prompt))
	if len(got) != 1 || got[0].Title != want {
		t.Fatalf("got %+v, want one session titled %q", got, want)
	}
}

func TestCodexUnclosedFenceInsideAdjacentWrapper(t *testing.T) {
	want := "Keep the real prompt after adjacent wrappers"
	prompt := "<environment_context>injected context</environment_context><skill>\n```\ninjected manual\n</skill>\n" + want
	got := codexTestScan(t, codexTestMeta(nil)+codexTestMessage("user", prompt))
	if len(got) != 1 || got[0].Title != want {
		t.Fatalf("got %+v, want one session titled %q", got, want)
	}
}

func TestCodexPreservesFenceAfterInlineWrapper(t *testing.T) {
	prompt := "Explain <skill>an inline example</skill><user_instructions>\n```xml\n</user_instructions>\n<skill>literal example</skill>\n```"
	want := "Explain an inline example ```xml </user_instructions> <skill>literal example</skill> ```"
	got := codexTestScan(t, codexTestMeta(nil)+codexTestMessage("user", prompt))
	if len(got) != 1 || got[0].Title != want {
		t.Fatalf("got %+v, want one session titled %q", got, want)
	}
}
