package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	bundled "github.com/aramponi/yakanban/skills"
)

func fakeLookPath(found ...string) LookPathFunc {
	set := map[string]bool{}
	for _, f := range found {
		set[f] = true
	}
	return func(file string) (string, error) {
		if set[file] {
			return "/usr/local/bin/" + file, nil
		}
		return "", os.ErrNotExist
	}
}

// newProject returns a project root and a home directory, neither of them the
// real ones: no test may write into the developer's own $HOME.
func newProject(t *testing.T) (root, home string) {
	t.Helper()
	base := t.TempDir()
	root = filepath.Join(base, "project")
	home = filepath.Join(base, "home")
	for _, dir := range []string{root, home} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root, home
}

func TestInstallCreatesTheSkillsInTheProject(t *testing.T) {
	root, home := newProject(t)
	o := Options{Root: root, Home: home, Agents: []Agent{Claude}}

	targets, err := o.Targets()
	if err != nil {
		t.Fatalf("Targets: %v", err)
	}
	results, err := Install(targets, "1.0.0", false)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if len(results) != len(bundled.Names()) {
		t.Fatalf("installed %d skills, want %d", len(results), len(bundled.Names()))
	}
	for _, r := range results {
		if r.Action != ActionWrote {
			t.Fatalf("%s: action = %q, want a fresh write", r.Path, r.Action)
		}
		if !strings.HasPrefix(r.Path, filepath.Join(root, ".claude", "skills")) {
			t.Fatalf("path = %q, want it under the project's .claude/skills", r.Path)
		}
		content, err := os.ReadFile(r.Path)
		if err != nil {
			t.Fatalf("reading back: %v", err)
		}
		if !strings.Contains(string(content), "yakanban-skill-version: 1.0.0") {
			t.Fatalf("%s carries no version marker", r.Path)
		}
	}
	if entries, err := os.ReadDir(home); err != nil || len(entries) != 0 {
		t.Fatalf("a project install must leave the home directory alone, found %v", entries)
	}
}

func TestInstallingTwiceChangesNothing(t *testing.T) {
	root, home := newProject(t)
	targets, err := Options{Root: root, Home: home, Agents: []Agent{Claude}}.Targets()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Install(targets, "1.0.0", false); err != nil {
		t.Fatal(err)
	}
	results, err := Install(targets, "1.0.0", false)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	for _, r := range results {
		if r.Action != ActionUpToDate {
			t.Fatalf("second install of %s = %q, want it to be a no-op", r.Path, r.Action)
		}
	}
}

func TestGlobalInstallLeavesTheProjectUntouched(t *testing.T) {
	root, home := newProject(t)
	targets, err := Options{Root: root, Home: home, Global: true, Agents: []Agent{Claude}}.Targets()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Install(targets, "1.0.0", false); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills", "yakanban", "SKILL.md")); err != nil {
		t.Fatalf("the global skill was not written: %v", err)
	}
	if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
		t.Fatalf("--global must not touch the project, found %v", entries)
	}
}

func TestAStaleInstallIsRefreshedButAnEditedOneIsNot(t *testing.T) {
	root, home := newProject(t)
	targets, err := Options{Root: root, Home: home, Agents: []Agent{Claude}, Skills: []string{"yakanban"}}.Targets()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Install(targets, "0.9.0", false); err != nil {
		t.Fatal(err)
	}

	statuses, err := Check(targets, "1.0.0")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if statuses[0].State != StateStale || !statuses[0].Stale() {
		t.Fatalf("state = %q, want stale", statuses[0].State)
	}
	if statuses[0].Installed != "0.9.0" {
		t.Fatalf("installed version = %q", statuses[0].Installed)
	}

	results, err := Update(targets, "1.0.0", false)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if results[0].Action != ActionUpdated {
		t.Fatalf("an untouched stale file should refresh, got %q", results[0].Action)
	}
}

func TestAnEditedSkillIsProtectedWithoutForce(t *testing.T) {
	root, home := newProject(t)
	targets, err := Options{Root: root, Home: home, Agents: []Agent{Claude}, Skills: []string{"yakanban"}}.Targets()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Install(targets, "0.9.0", false); err != nil {
		t.Fatal(err)
	}
	edited := "# my own notes\n"
	if err := os.WriteFile(targets[0].Path, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}

	statuses, err := Check(targets, "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if statuses[0].State != StateModified {
		t.Fatalf("state = %q, want modified", statuses[0].State)
	}

	results, err := Install(targets, "1.0.0", false)
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Action != ActionSkipped {
		t.Fatalf("action = %q, want the edit to be preserved", results[0].Action)
	}
	if content, _ := os.ReadFile(targets[0].Path); string(content) != edited {
		t.Fatal("the user's edit was overwritten without --force")
	}

	if _, err := Install(targets, "1.0.0", true); err != nil {
		t.Fatal(err)
	}
	if content, _ := os.ReadFile(targets[0].Path); string(content) == edited {
		t.Fatal("--force should have overwritten the file")
	}
}

func TestAFileWithNoMarkerIsTreatedAsModified(t *testing.T) {
	root, home := newProject(t)
	targets, err := Options{Root: root, Home: home, Agents: []Agent{Claude}, Skills: []string{"yakanban"}}.Targets()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(targets[0].Path), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, content := range []string{
		"no marker at all\n",
		"<!-- yakanban-skill-version: -->\n",
		"<!-- yakanban-skill-version: 1.0.0 sha256:notahash -->\n",
	} {
		if err := os.WriteFile(targets[0].Path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		statuses, err := Check(targets, "1.0.0")
		if err != nil {
			t.Fatalf("an unparseable marker must not crash Check: %v", err)
		}
		if statuses[0].State != StateModified {
			t.Fatalf("content %q gave state %q, want modified", content, statuses[0].State)
		}
	}
}

func TestUpdateDoesNotCreateWhatWasNeverInstalled(t *testing.T) {
	root, home := newProject(t)
	targets, err := Options{Root: root, Home: home, Agents: []Agent{Claude}}.Targets()
	if err != nil {
		t.Fatal(err)
	}
	results, err := Update(targets, "1.0.0", false)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	for _, r := range results {
		if r.Action != ActionNotInstalled {
			t.Fatalf("action = %q, want update to create nothing", r.Action)
		}
		if _, err := os.Stat(r.Path); err == nil {
			t.Fatalf("%s was created by update", r.Path)
		}
	}
}

func TestCheckReportsMissingWithoutFailing(t *testing.T) {
	root, home := newProject(t)
	targets, err := Options{Root: root, Home: home, Agents: []Agent{Claude}}.Targets()
	if err != nil {
		t.Fatal(err)
	}
	statuses, err := Check(targets, "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range statuses {
		if s.State != StateMissing {
			t.Fatalf("state = %q, want missing", s.State)
		}
		if s.Stale() {
			t.Fatal("a skill nobody installed must not fail check; every fresh clone would")
		}
	}
}

func TestPathSkipsAgentDetection(t *testing.T) {
	root, home := newProject(t)
	dir := filepath.Join(root, "somewhere")
	targets, err := Options{Root: root, Home: home, Path: dir}.Targets()
	if err != nil {
		t.Fatalf("Targets: %v", err)
	}
	for _, tgt := range targets {
		if tgt.Agent != "" {
			t.Fatalf("--path should not attribute a target to an agent, got %q", tgt.Agent)
		}
		if !strings.HasPrefix(tgt.Path, dir) {
			t.Fatalf("path = %q, want it under %q", tgt.Path, dir)
		}
	}
}

func TestDetectionUsesThePathAndTheHomeDirectory(t *testing.T) {
	_, home := newProject(t)
	if Present(Claude, home, fakeLookPath()) {
		t.Fatal("nothing on PATH and no config directory should not count as present")
	}
	if !Present(Claude, home, fakeLookPath("claude")) {
		t.Fatal("an executable on PATH should count as present")
	}
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !Present(Claude, home, fakeLookPath()) {
		t.Fatal("an existing config directory should count as present")
	}
}

func TestNoAgentDetectedIsReportedNotGuessed(t *testing.T) {
	root, home := newProject(t)
	_, err := Options{Root: root, Home: home, LookPath: fakeLookPath()}.Targets()
	if err == nil || !strings.Contains(err.Error(), "--agent") {
		t.Fatalf("err = %v, want it to say how to name an agent instead of picking one", err)
	}
}

func TestUnknownSkillAndAgentAreRefused(t *testing.T) {
	root, home := newProject(t)
	if _, err := (Options{Root: root, Home: home, Skills: []string{"nope"}}).Targets(); err == nil {
		t.Fatal("an unknown skill name should be refused")
	}
	if _, err := ParseAgent("emacs"); err == nil {
		t.Fatal("an unknown agent should be refused")
	}
}

func TestEveryAgentHasItsOwnDirectories(t *testing.T) {
	root, home := newProject(t)
	seen := map[string]bool{}
	for _, a := range Agents {
		project := a.ProjectDir(root)
		global := a.GlobalDir(home)
		if project == "" || global == "" {
			t.Fatalf("%s has no directory", a)
		}
		if !strings.HasPrefix(project, root) || !strings.HasPrefix(global, home) {
			t.Fatalf("%s escapes its root: %q / %q", a, project, global)
		}
		seen[string(a)] = true
	}
	if len(seen) != len(Agents) {
		t.Fatal("the agent list has duplicates")
	}
}

func TestBundledSkillsAreTheOnesInTheRepository(t *testing.T) {
	for _, name := range bundled.Names() {
		sk, err := bundled.Get(name)
		if err != nil {
			t.Fatalf("Get(%q): %v", name, err)
		}
		onDisk, err := os.ReadFile(filepath.Join("..", "..", "skills", name, "SKILL.md"))
		if err != nil {
			t.Fatalf("reading skills/%s/SKILL.md: %v", name, err)
		}
		if string(sk.Content) != string(onDisk) {
			t.Fatalf("the embedded %s has drifted from skills/%s/SKILL.md", name, name)
		}
	}
}

func TestDetectReportsEveryAgentAndWhy(t *testing.T) {
	_, home := newProject(t)
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}

	detected := Detect(home, fakeLookPath("pi"))

	if len(detected) != len(Agents) {
		t.Fatalf("Detect returned %d agents, want every known one: an agent that is\nabsent must still be listed, or it looks unsupported", len(detected))
	}
	byAgent := map[Agent]Detection{}
	for _, d := range detected {
		byAgent[d.Agent] = d
	}
	if d := byAgent[Pi]; !d.Found || d.Reason != "pi on PATH" {
		t.Fatalf("pi = %+v, want it found via PATH with that said", d)
	}
	if d := byAgent[Claude]; !d.Found || d.Reason != "~/.claude" {
		t.Fatalf("claude = %+v, want it found via its config directory", d)
	}
	if d := byAgent[Cursor]; d.Found || d.Reason != "" {
		t.Fatalf("cursor = %+v, want it listed as absent with no reason", d)
	}
}

func TestPiUserPathIsNotTheProjectPathUnderHome(t *testing.T) {
	root, home := newProject(t)

	project, err := (Options{Root: root, Home: home, Agents: []Agent{Pi}, Skills: []string{"yakanban"}}).Targets()
	if err != nil {
		t.Fatal(err)
	}
	global, err := (Options{Root: root, Home: home, Global: true, Agents: []Agent{Pi}, Skills: []string{"yakanban"}}).Targets()
	if err != nil {
		t.Fatal(err)
	}

	wantProject := filepath.Join(root, ".pi", "skills", "yakanban", "SKILL.md")
	wantGlobal := filepath.Join(home, ".pi", "agent", "skills", "yakanban", "SKILL.md")
	if project[0].Path != wantProject {
		t.Fatalf("project path = %q, want %q", project[0].Path, wantProject)
	}
	if global[0].Path != wantGlobal {
		t.Fatalf("user path = %q, want %q — pi is the one agent whose user path is not its project path under $HOME",
			global[0].Path, wantGlobal)
	}
}

func TestHermesProjectInstallSaysItMustBeTrusted(t *testing.T) {
	root, home := newProject(t)

	project, err := (Options{Root: root, Home: home, Agents: []Agent{Hermes}, Skills: []string{"yakanban"}}).Targets()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(project[0].Note, "hermes skills trust") {
		t.Fatalf("note = %q, want it to say the file alone will not load", project[0].Note)
	}
	results, err := Install(project, "1.0.0", false)
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Note != project[0].Note {
		t.Fatal("the note must survive into the result, or nothing reports it")
	}

	global, err := (Options{Root: root, Home: home, Global: true, Agents: []Agent{Hermes}, Skills: []string{"yakanban"}}).Targets()
	if err != nil {
		t.Fatal(err)
	}
	if global[0].Note != "" {
		t.Fatalf("note = %q, want none: trust is about project discovery", global[0].Note)
	}
}

func TestCheckCarriesTheTrustNote(t *testing.T) {
	root, home := newProject(t)
	targets, err := (Options{Root: root, Home: home, Agents: []Agent{Hermes}, Skills: []string{"yakanban"}}).Targets()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Install(targets, "1.0.0", false); err != nil {
		t.Fatal(err)
	}
	statuses, err := Check(targets, "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if statuses[0].State != StateCurrent {
		t.Fatalf("state = %q", statuses[0].State)
	}
	if !strings.Contains(statuses[0].Note, "trust") {
		t.Fatal("check must not present an untrusted hermes skill as simply current")
	}
}

func TestTheNewAgentsAreSelectable(t *testing.T) {
	for _, name := range []string{"hermes", "pi", "HERMES"} {
		if _, err := ParseAgent(name); err != nil {
			t.Fatalf("ParseAgent(%q): %v", name, err)
		}
	}
}
