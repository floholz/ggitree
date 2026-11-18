// Package t3setup adds ggitree's action to T3 Code for every project, and takes
// it out again.
//
// T3 Code keeps its config in <userdata>/settings.json and keybindings.json.
// The action list shared by all projects is the top-level
// "defaultProjectScripts" array. The settings page cannot edit it; it only
// edits a project's own list, projectSettingsOverrides[<id>].defaultProjectScripts.
// A project with its own list ignores the shared one, so the action is added to
// those lists as well.
//
// Install is idempotent: it inserts or replaces entries by id and leaves a file
// untouched when nothing changed. The first install copies both files to
// <userdata>/ggitree-backup. Uninstall restores that copy when the file is
// exactly as install left it; otherwise it removes only ggitree's entries, so
// later changes to T3's settings are kept.
package t3setup

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

const (
	ScriptID          = "ggitree"
	KeybindingCommand = "script." + ScriptID + ".run"
	DefaultKey        = "mod+alt+g"

	settingsFile    = "settings.json"
	keybindingsFile = "keybindings.json"
	backupDirName   = "ggitree-backup"
	stateFile       = "state.json"
)

// Config says where T3 Code's files are and what the action opens.
type Config struct {
	Dir        string // T3 Code's userdata folder
	PreviewURL string
	Log        io.Writer
}

// DefaultDir is $T3CODE_HOME/userdata, or ~/.t3/userdata.
func DefaultDir() string {
	if home := os.Getenv("T3CODE_HOME"); home != "" {
		return filepath.Join(home, "userdata")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".t3", "userdata")
}

// Script is the action as T3 Code stores it.
func Script(previewURL string) *Object {
	o := &Object{}
	o.Set("id", ScriptID)
	o.Set("name", "GGitree")
	o.Set("command", "ggitree open; exit")
	o.Set("icon", "play")
	o.Set("runOnWorktreeCreate", false)
	o.Set("stopOnSettle", true)
	o.Set("previewUrl", previewURL)
	o.Set("autoOpenPreview", true)
	return o
}

type state struct {
	InstalledAt string               `json:"installedAt"`
	Files       map[string]fileState `json:"files"`
}

type fileState struct {
	Existed bool   `json:"existed"` // whether T3 had the file before the first install
	SHA256  string `json:"sha256"`  // the file as the last install left it
}

type editor struct {
	name    string
	empty   string // contents to start from when the file does not exist
	install func(root any, c Config) (changes []string, err error)
	remove  func(root any) (changes []string, err error)
}

var editors = []editor{
	{settingsFile, "{}", installSettings, removeSettings},
	{keybindingsFile, "[]", installKeybinding, removeKeybinding},
}

// Install adds the action and its shortcut. Running it again only repairs what
// is missing or out of date.
func Install(c Config) error {
	if fi, err := os.Stat(c.Dir); err != nil || !fi.IsDir() {
		return fmt.Errorf("T3 Code's config folder %s was not found (set it with --t3-dir)", c.Dir)
	}
	backupDir := filepath.Join(c.Dir, backupDirName)
	st, err := loadState(backupDir)
	if err != nil {
		return err
	}
	if st == nil {
		st, err = makeBackup(c.Dir, backupDir)
		if err != nil {
			return err
		}
		logf(c, "backed up T3 Code's settings to %s", backupDir)
	}

	for _, ed := range editors {
		path := filepath.Join(c.Dir, ed.name)
		data, err := os.ReadFile(path)
		if errors.Is(err, fs.ErrNotExist) {
			data = []byte(ed.empty)
		} else if err != nil {
			return err
		}
		root, err := Decode(data)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		changes, err := ed.install(root, c)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if len(changes) > 0 {
			if data, err = Encode(root); err != nil {
				return err
			}
			if err := writeAtomic(path, data); err != nil {
				return err
			}
			for _, ch := range changes {
				logf(c, "%s: %s", ed.name, ch)
			}
		} else {
			logf(c, "%s: already up to date", ed.name)
		}
		fsState := st.Files[ed.name]
		fsState.SHA256 = digest(data)
		st.Files[ed.name] = fsState
	}
	return saveState(backupDir, st)
}

// Uninstall takes the action and its shortcut out again.
func Uninstall(c Config) error {
	backupDir := filepath.Join(c.Dir, backupDirName)
	st, err := loadState(backupDir)
	if err != nil {
		return err
	}
	if st == nil {
		logf(c, "no backup in %s; removing only ggitree's entries", backupDir)
	}
	restoredAll := st != nil
	for _, ed := range editors {
		path := filepath.Join(c.Dir, ed.name)
		data, err := os.ReadFile(path)
		missing := errors.Is(err, fs.ErrNotExist)
		if err != nil && !missing {
			return err
		}

		if st != nil {
			fsState, known := st.Files[ed.name]
			if known && !missing && digest(data) == fsState.SHA256 {
				if fsState.Existed {
					saved, err := os.ReadFile(filepath.Join(backupDir, ed.name))
					if err != nil {
						return fmt.Errorf("reading backup: %w", err)
					}
					if err := writeAtomic(path, saved); err != nil {
						return err
					}
					logf(c, "%s: restored from backup", ed.name)
				} else {
					if err := os.Remove(path); err != nil {
						return err
					}
					logf(c, "%s: removed (T3 Code had none before)", ed.name)
				}
				continue
			}
			restoredAll = false
		}

		if missing {
			continue
		}
		root, err := Decode(data)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		changes, err := ed.remove(root)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if len(changes) == 0 {
			logf(c, "%s: nothing to remove", ed.name)
			continue
		}
		if data, err = Encode(root); err != nil {
			return err
		}
		if err := writeAtomic(path, data); err != nil {
			return err
		}
		for _, ch := range changes {
			logf(c, "%s: %s", ed.name, ch)
		}
		if st != nil {
			logf(c, "%s: changed since install, so only ggitree's entries were removed", ed.name)
		}
	}

	if st == nil {
		return nil
	}
	if restoredAll {
		return os.RemoveAll(backupDir)
	}
	// Keep the old copy for reference, but out of the way of the next install.
	kept := backupDir + "-" + time.Now().Format("20060102-150405")
	if err := os.Rename(backupDir, kept); err != nil {
		return err
	}
	logf(c, "the backup from the first install is kept in %s", kept)
	return nil
}

// Settings

// scriptLists returns every action list T3 Code may read for a project: the
// shared one (created if absent) and each project's own list.
func scriptLists(root *Object, create bool) []namedList {
	var lists []namedList
	if v, _ := root.Get("defaultProjectScripts"); v != nil {
		if a, ok := v.(*Array); ok {
			lists = append(lists, namedList{"all projects", a})
		}
	} else if create {
		a := &Array{Items: []any{}}
		root.Set("defaultProjectScripts", a)
		lists = append(lists, namedList{"all projects", a})
	}
	if v, _ := root.Get("projectSettingsOverrides"); v != nil {
		if o, ok := v.(*Object); ok {
			for _, id := range o.Keys() {
				p, _ := o.Get(id)
				if po, ok := p.(*Object); ok {
					if s, _ := po.Get("defaultProjectScripts"); s != nil {
						if a, ok := s.(*Array); ok {
							lists = append(lists, namedList{"project " + id, a})
						}
					}
				}
			}
		}
	}
	// Older T3 Code versions kept project lists here, and still read them until
	// the settings have been migrated ("projectSettingsFolded").
	folded, _ := root.Get("projectSettingsFolded")
	if folded != true || !create {
		if v, _ := root.Get("projectScriptOverrides"); v != nil {
			if o, ok := v.(*Object); ok {
				for _, id := range o.Keys() {
					p, _ := o.Get(id)
					if a, ok := p.(*Array); ok {
						lists = append(lists, namedList{"project " + id + " (legacy)", a})
					}
				}
			}
		}
	}
	return lists
}

type namedList struct {
	name string
	list *Array
}

func installSettings(root any, c Config) ([]string, error) {
	obj, ok := root.(*Object)
	if !ok {
		return nil, errors.New("expected a JSON object")
	}
	want := Script(c.PreviewURL)
	wantJSON, _ := marshal(want)
	var changes []string
	for _, l := range scriptLists(obj, true) {
		i := indexByID(l.list)
		switch {
		case i < 0:
			l.list.Items = append(l.list.Items, want)
			changes = append(changes, "added the action to "+l.name)
		default:
			have, _ := marshal(l.list.Items[i])
			if !bytes.Equal(have, wantJSON) {
				l.list.Items[i] = want
				changes = append(changes, "updated the action in "+l.name)
			}
		}
	}
	return changes, nil
}

func removeSettings(root any) ([]string, error) {
	obj, ok := root.(*Object)
	if !ok {
		return nil, errors.New("expected a JSON object")
	}
	var changes []string
	for _, l := range scriptLists(obj, false) {
		if i := indexByID(l.list); i >= 0 {
			l.list.Items = append(l.list.Items[:i], l.list.Items[i+1:]...)
			changes = append(changes, "removed the action from "+l.name)
		}
	}
	return changes, nil
}

func indexByID(a *Array) int {
	for i, item := range a.Items {
		if o, ok := item.(*Object); ok {
			if id, _ := o.Get("id"); id == ScriptID {
				return i
			}
		}
	}
	return -1
}

// Keybindings

func installKeybinding(root any, _ Config) ([]string, error) {
	arr, ok := root.(*Array)
	if !ok {
		return nil, errors.New("expected a JSON array")
	}
	// An existing shortcut is left alone, so a rebinding in T3 survives reinstalls.
	if indexByCommand(arr) >= 0 {
		return nil, nil
	}
	kb := &Object{}
	kb.Set("key", DefaultKey)
	kb.Set("command", KeybindingCommand)
	arr.Items = append(arr.Items, kb)
	return []string{"added the shortcut " + DefaultKey}, nil
}

func removeKeybinding(root any) ([]string, error) {
	arr, ok := root.(*Array)
	if !ok {
		return nil, errors.New("expected a JSON array")
	}
	var changes []string
	for i := indexByCommand(arr); i >= 0; i = indexByCommand(arr) {
		arr.Items = append(arr.Items[:i], arr.Items[i+1:]...)
		changes = append(changes, "removed the shortcut")
	}
	return changes, nil
}

func indexByCommand(a *Array) int {
	for i, item := range a.Items {
		if o, ok := item.(*Object); ok {
			if cmd, _ := o.Get("command"); cmd == KeybindingCommand {
				return i
			}
		}
	}
	return -1
}

// Files

func makeBackup(dir, backupDir string) (*state, error) {
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		return nil, err
	}
	st := &state{InstalledAt: time.Now().Format(time.RFC3339), Files: map[string]fileState{}}
	for _, ed := range editors {
		data, err := os.ReadFile(filepath.Join(dir, ed.name))
		if errors.Is(err, fs.ErrNotExist) {
			st.Files[ed.name] = fileState{Existed: false}
			continue
		} else if err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(backupDir, ed.name), data, 0o600); err != nil {
			return nil, err
		}
		st.Files[ed.name] = fileState{Existed: true}
	}
	return st, saveState(backupDir, st)
}

func loadState(backupDir string) (*state, error) {
	data, err := os.ReadFile(filepath.Join(backupDir, stateFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	var st state
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Join(backupDir, stateFile), err)
	}
	if st.Files == nil {
		st.Files = map[string]fileState{}
	}
	return &st, nil
}

func saveState(backupDir string, st *state) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(backupDir, stateFile), append(data, '\n'))
}

// writeAtomic replaces path in one step, so T3 Code never reads a half-written
// file. A symlinked file is written through to its target.
func writeAtomic(path string, data []byte) error {
	if target, err := filepath.EvalSymlinks(path); err == nil {
		path = target
	}
	mode := fs.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".ggitree-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func logf(c Config, format string, args ...any) {
	if c.Log != nil {
		fmt.Fprintf(c.Log, format+"\n", args...)
	}
}
