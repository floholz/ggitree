package t3setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const settingsBefore = `{
  "favorites": [
    {
      "provider": "claudeAgent",
      "model": "claude-opus-5-5"
    }
  ],
  "projectSettingsOverrides": {
    "p1": {
      "defaultProjectScripts": [
        {
          "id": "lazygit",
          "name": "lazygit",
          "command": "lazygit"
        }
      ]
    },
    "p2": {
      "defaultAutoPull": true
    }
  },
  "projectSettingsFolded": true,
  "sidebarAutoSettleAfterDays": 7,
  "note": "<html> & stays unescaped"
}
`

const keybindingsBefore = `[
  {
    "key": "mod+b",
    "command": "sidebar.toggle"
  }
]
`

func setup(t *testing.T) Config {
	t.Helper()
	dir := t.TempDir()
	write(t, filepath.Join(dir, settingsFile), settingsBefore)
	write(t, filepath.Join(dir, keybindingsFile), keybindingsBefore)
	return Config{Dir: dir, PreviewURL: "http://127.0.0.1:4747"}
}

func write(t *testing.T, path, s string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestInstallAddsActionEverywhereAndKeepsOrder(t *testing.T) {
	c := setup(t)
	if err := Install(c); err != nil {
		t.Fatal(err)
	}
	s := read(t, filepath.Join(c.Dir, settingsFile))
	root, err := Decode([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	obj := root.(*Object)
	keys := strings.Join(obj.Keys(), ",")
	if keys != "favorites,projectSettingsOverrides,projectSettingsFolded,sidebarAutoSettleAfterDays,note,defaultProjectScripts" {
		t.Errorf("key order changed: %s", keys)
	}
	lists := scriptLists(obj, false)
	if len(lists) != 2 {
		t.Fatalf("want the shared list and p1's list, got %d", len(lists))
	}
	for _, l := range lists {
		if indexByID(l.list) < 0 {
			t.Errorf("%s is missing the action", l.name)
		}
	}
	if !strings.Contains(s, `"<html> & stays unescaped"`) || !strings.Contains(s, `"sidebarAutoSettleAfterDays": 7,`) {
		t.Errorf("values were rewritten:\n%s", s)
	}
	if kb := read(t, filepath.Join(c.Dir, keybindingsFile)); !strings.Contains(kb, KeybindingCommand) {
		t.Errorf("shortcut missing:\n%s", kb)
	}
}

func TestInstallIsIdempotent(t *testing.T) {
	c := setup(t)
	if err := Install(c); err != nil {
		t.Fatal(err)
	}
	s1 := read(t, filepath.Join(c.Dir, settingsFile))
	k1 := read(t, filepath.Join(c.Dir, keybindingsFile))
	if err := Install(c); err != nil {
		t.Fatal(err)
	}
	if s2 := read(t, filepath.Join(c.Dir, settingsFile)); s2 != s1 {
		t.Errorf("second install changed settings:\n%s", s2)
	}
	if k2 := read(t, filepath.Join(c.Dir, keybindingsFile)); k2 != k1 {
		t.Errorf("second install changed keybindings:\n%s", k2)
	}
	// The backup still holds the files from before the first install.
	if b := read(t, filepath.Join(c.Dir, backupDirName, settingsFile)); b != settingsBefore {
		t.Errorf("backup was overwritten:\n%s", b)
	}
}

func TestInstallRepairsOutdatedAction(t *testing.T) {
	c := setup(t)
	if err := Install(c); err != nil {
		t.Fatal(err)
	}
	c.PreviewURL = "http://127.0.0.1:5000"
	if err := Install(c); err != nil {
		t.Fatal(err)
	}
	s := read(t, filepath.Join(c.Dir, settingsFile))
	if strings.Contains(s, "4747") || strings.Count(s, "5000") != 2 {
		t.Errorf("action not updated in both lists:\n%s", s)
	}
}

func TestUninstallRestoresBackup(t *testing.T) {
	c := setup(t)
	if err := Install(c); err != nil {
		t.Fatal(err)
	}
	if err := Uninstall(c); err != nil {
		t.Fatal(err)
	}
	if s := read(t, filepath.Join(c.Dir, settingsFile)); s != settingsBefore {
		t.Errorf("settings not restored:\n%s", s)
	}
	if k := read(t, filepath.Join(c.Dir, keybindingsFile)); k != keybindingsBefore {
		t.Errorf("keybindings not restored:\n%s", k)
	}
	if _, err := os.Stat(filepath.Join(c.Dir, backupDirName)); !os.IsNotExist(err) {
		t.Errorf("backup folder left behind")
	}
}

func TestUninstallKeepsLaterChanges(t *testing.T) {
	c := setup(t)
	if err := Install(c); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(c.Dir, settingsFile)
	write(t, path, strings.Replace(read(t, path), `"sidebarAutoSettleAfterDays": 7`, `"sidebarAutoSettleAfterDays": 3`, 1))
	if err := Uninstall(c); err != nil {
		t.Fatal(err)
	}
	s := read(t, path)
	if strings.Contains(s, `"id": "ggitree"`) {
		t.Errorf("action still present:\n%s", s)
	}
	if !strings.Contains(s, `"sidebarAutoSettleAfterDays": 3`) {
		t.Errorf("later change lost:\n%s", s)
	}
	// Unchanged keybindings are restored exactly.
	if k := read(t, filepath.Join(c.Dir, keybindingsFile)); k != keybindingsBefore {
		t.Errorf("keybindings not restored:\n%s", k)
	}
	matches, _ := filepath.Glob(filepath.Join(c.Dir, backupDirName+"-*"))
	if len(matches) != 1 {
		t.Errorf("want the old backup kept aside, got %v", matches)
	}
}

func TestInstallWithoutFiles(t *testing.T) {
	c := Config{Dir: t.TempDir(), PreviewURL: "http://127.0.0.1:4747"}
	if err := Install(c); err != nil {
		t.Fatal(err)
	}
	if err := Uninstall(c); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{settingsFile, keybindingsFile} {
		if _, err := os.Stat(filepath.Join(c.Dir, f)); !os.IsNotExist(err) {
			t.Errorf("%s should be gone again", f)
		}
	}
}

func TestLegacyListsBeforeMigration(t *testing.T) {
	c := Config{Dir: t.TempDir(), PreviewURL: "http://127.0.0.1:4747"}
	write(t, filepath.Join(c.Dir, settingsFile), `{"projectScriptOverrides": {"p1": [], "p2": null}}`)
	if err := Install(c); err != nil {
		t.Fatal(err)
	}
	s := read(t, filepath.Join(c.Dir, settingsFile))
	if strings.Count(s, `"id": "ggitree"`) != 2 || !strings.Contains(s, `"p2": null`) {
		t.Errorf("want the shared list and p1's legacy list, p2 untouched:\n%s", s)
	}
}

func TestMissingDir(t *testing.T) {
	err := Install(Config{Dir: filepath.Join(t.TempDir(), "nope")})
	if err == nil || !strings.Contains(err.Error(), "--t3-dir") {
		t.Fatalf("want a hint about --t3-dir, got %v", err)
	}
}
