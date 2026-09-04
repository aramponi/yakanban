package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/aramponi/yakanban/internal/skill"
)

// menuOptions builds a deterministic machine: claude has a config directory,
// codex is on PATH, nothing else is there.
func menuOptions(t *testing.T) skill.Options {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	return skill.Options{
		Home: home,
		LookPath: func(file string) (string, error) {
			if file == "codex" {
				return "/usr/bin/codex", nil
			}
			return "", os.ErrNotExist
		},
	}
}

func runMenu(t *testing.T, input string) ([]skill.Agent, bool, string) {
	t.Helper()
	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetIn(strings.NewReader(input))
	cmd.SetOut(&out)
	e := &env{}
	agents, ok, err := e.chooseAgents(cmd, menuOptions(t))
	if err != nil {
		t.Fatalf("chooseAgents: %v", err)
	}
	return agents, ok, out.String()
}

func agentNames(agents []skill.Agent) string {
	out := make([]string, 0, len(agents))
	for _, a := range agents {
		out = append(out, string(a))
	}
	return strings.Join(out, ",")
}

func TestTheMenuListsEveryAgentWithTheReasonItIsTicked(t *testing.T) {
	_, _, out := runMenu(t, "q\n")

	for _, agent := range skill.Agents {
		if !strings.Contains(out, string(agent)) {
			t.Fatalf("%s is missing from the menu; an unseen agent and an\nunsupported one must not look the same:\n%s", agent, out)
		}
	}
	if !strings.Contains(out, "found: codex on PATH") {
		t.Fatalf("the menu should say why codex is ticked:\n%s", out)
	}
	if !strings.Contains(out, "found: ~/.claude") {
		t.Fatalf("the menu should say why claude is ticked:\n%s", out)
	}
	if !strings.Contains(out, "not detected") {
		t.Fatalf("absent agents should say so:\n%s", out)
	}
}

func TestEnterAcceptsTheDetectedAgents(t *testing.T) {
	agents, ok, _ := runMenu(t, "\n")
	if !ok {
		t.Fatal("Enter should accept the selection")
	}
	if agentNames(agents) != "claude,codex" {
		t.Fatalf("selection = %s, want the detected ones", agentNames(agents))
	}
}

func TestASelectionCanBeEditedBeforeInstalling(t *testing.T) {
	// 1 unticks claude, 3 ticks cursor, then install.
	agents, ok, _ := runMenu(t, "1\n3\n\n")
	if !ok {
		t.Fatal("the menu should have accepted")
	}
	if agentNames(agents) != "codex,cursor" {
		t.Fatalf("selection = %s, want claude dropped and cursor added", agentNames(agents))
	}
}

func TestCancellingWritesNothing(t *testing.T) {
	agents, ok, _ := runMenu(t, "q\n")
	if ok || agents != nil {
		t.Fatalf("q should cancel, got %v / %v", agents, ok)
	}
}

func TestAnEmptySelectionIsRefusedRatherThanInstallingNothing(t *testing.T) {
	// Untick both detected agents, press Enter, then give up.
	agents, ok, out := runMenu(t, "1\n2\n\nq\n")
	if ok {
		t.Fatalf("an empty selection should not install, got %v", agents)
	}
	if !strings.Contains(out, "nothing selected") {
		t.Fatalf("the menu should say why it did not proceed:\n%s", out)
	}
}

func TestAnUnusableAnswerIsReportedAndTheMenuStays(t *testing.T) {
	_, ok, out := runMenu(t, "42\nbanana\nq\n")
	if ok {
		t.Fatal("the menu should still have been cancelled")
	}
	if !strings.Contains(out, "not one of 1-") {
		t.Fatalf("an out-of-range answer should be reported:\n%s", out)
	}
	if strings.Count(out, "Install the yakanban skills for:") != 3 {
		t.Fatalf("the menu should be redrawn after each bad answer:\n%s", out)
	}
}

// Input ending mid-question must not be read as consent.
func TestEndOfInputCancels(t *testing.T) {
	agents, ok, _ := runMenu(t, "")
	if ok || agents != nil {
		t.Fatal("an empty stream should cancel, not install")
	}
}
