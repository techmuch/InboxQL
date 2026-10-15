package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The frontmatter every tool requires, and a name matching the folder, which
// Gemini CLI asks for.
func TestSkillIsWellFormed(t *testing.T) {
	c := Content("1.2.3")
	if !strings.HasPrefix(c, "---\nname: iql\ndescription: ") {
		t.Fatalf("frontmatter: %q", c[:60])
	}
	desc := strings.SplitN(strings.SplitN(c, "description: ", 2)[1], "\n", 2)[0]
	if len(desc) > 1024 {
		t.Errorf("description is %d characters", len(desc))
	}
	if !strings.Contains(c, "Installed by iql 1.2.3.") || strings.Contains(c, "{{VERSION}}") {
		t.Error("version not stamped")
	}
	for _, must := range []string{"iql help agents", "--origin agent", "Send only when the user asks", "-label:x"} {
		if !strings.Contains(c, must) {
			t.Errorf("skill does not mention %q", must)
		}
	}
}

func TestInstallRefreshRemove(t *testing.T) {
	home := t.TempDir()
	l, _ := Find(home, "claude")
	st, err := Install(l, "1.0.0")
	if err != nil || !st.Installed || !st.Current || st.Version != "1.0.0" {
		t.Fatalf("%+v %v", st, err)
	}
	if st := Status(home, "1.1.0")[0]; st.Current {
		t.Error("an old version reads as current")
	}
	if done := Refresh(home, "1.1.0"); len(done) != 1 {
		t.Errorf("refreshed %v", done)
	}
	if st := Status(home, "1.1.0")[0]; !st.Current || st.Version != "1.1.0" {
		t.Errorf("after refresh %+v", st)
	}
	// Refresh never installs where there was nothing.
	if a := Status(home, "1.1.0")[1]; a.Installed {
		t.Error("refresh installed into agents")
	}
	if _, err := Remove(l, "1.1.0"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Dir(l.Path)); !os.IsNotExist(err) {
		t.Error("the empty folder was left behind")
	}
}

// A skill named iql that somebody else wrote is never overwritten or removed.
func TestForeignSkillIsLeftAlone(t *testing.T) {
	home := t.TempDir()
	l, _ := Find(home, "agents")
	os.MkdirAll(filepath.Dir(l.Path), 0o755)
	os.WriteFile(l.Path, []byte("---\nname: iql\ndescription: mine\n---\n"), 0o644)
	if _, err := Install(l, "1.0.0"); err != ErrForeign {
		t.Errorf("install over foreign: %v", err)
	}
	if _, err := Remove(l, "1.0.0"); err != ErrForeign {
		t.Errorf("remove foreign: %v", err)
	}
	if b, _ := os.ReadFile(l.Path); string(b) != "---\nname: iql\ndescription: mine\n---\n" {
		t.Error("foreign skill was changed")
	}
}

func TestDetectByFolder(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".codex"), 0o755)
	got := map[string]bool{}
	for _, tool := range Detect(home) {
		got[tool.ID] = tool.Installed
	}
	if !got["codex"] || got["claude-code"] || got["gemini"] {
		t.Errorf("detected %v", got)
	}
}
