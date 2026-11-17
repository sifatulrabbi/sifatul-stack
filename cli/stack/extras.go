package stack

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

const extrasFile = "extras.json"

type Extra struct {
	Name   string   `json:"-"`
	Repo   string   `json:"repo"`
	Branch string   `json:"branch,omitempty"`
	Skills []string `json:"skills"`
}

var (
	extraName  = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
	skillName  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	branchName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)
)

func LoadExtras(source string) ([]Extra, error) {
	raw, err := os.ReadFile(filepath.Join(source, extrasFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	bad := func(format string, a ...any) error { return fmt.Errorf(extrasFile+": "+format, a...) }
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var byName map[string]Extra
	if err := dec.Decode(&byName); err != nil {
		return nil, bad("%v", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, bad("want one object, found more after it")
	}
	var out []Extra
	for _, name := range slices.Sorted(maps.Keys(byName)) {
		e := byName[name]
		e.Name = name
		switch {
		case !extraName.MatchString(name):
			return nil, bad("extra name %q must match %s", name, extraName)
		case e.Repo == "":
			return nil, bad("%s has no repo", name)
		case e.Branch != "" && !branchName.MatchString(e.Branch):
			return nil, bad("%s branch %q must match %s", name, e.Branch, branchName)
		case len(e.Skills) == 0:
			return nil, bad("%s lists no skills", name)
		}
		for i, s := range e.Skills {
			if !skillName.MatchString(s) {
				return nil, bad("%s skill %q must be a folder name matching %s", name, s, skillName)
			}
			if slices.Contains(e.Skills[:i], s) {
				return nil, bad("%s lists %s twice", name, s)
			}
		}
		out = append(out, e)
	}
	return out, nil
}

var errNotCloned = errors.New("not cloned")

type skillLink struct {
	name string
	src  string
	err  error
}

func (s Syncer) skillLinks() ([]skillLink, error) {
	names, err := s.skills()
	if err != nil {
		return nil, err
	}
	const fromStack = ""
	owner := map[string]string{}
	var out []skillLink
	for _, n := range names {
		out = append(out, skillLink{name: n, src: filepath.Join(s.Source, "skills", n)})
		owner[n] = fromStack
	}
	for _, e := range s.Extras {
		dir := filepath.Join(s.ExtrasDir, e.Name)
		found, ferr := findSkills(dir)
		for _, n := range e.Skills {
			l := skillLink{name: n}
			by, taken := owner[n]
			switch {
			case taken && by == fromStack:
				l.err = fmt.Errorf("extra %s lists %s, but the stack has its own %s", e.Name, n, n)
			case taken:
				l.err = fmt.Errorf("extra %s lists %s, but extra %s already links %s", e.Name, n, by, n)
			case errors.Is(ferr, os.ErrNotExist):
				l.err = fmt.Errorf("extra %s is %w; run `sifatul-stack install`", e.Name, errNotCloned)
			case ferr != nil:
				l.err = ferr
			case len(found[n]) == 0:
				l.err = fmt.Errorf("no %s/SKILL.md in extra %s", n, e.Name)
			case len(found[n]) > 1:
				var rel []string
				for _, p := range found[n] {
					r, _ := filepath.Rel(dir, p)
					rel = append(rel, r)
				}
				l.err = fmt.Errorf("extra %s has more than one %s: %s", e.Name, n, strings.Join(rel, ", "))
			default:
				l.src = found[n][0]
				owner[n] = e.Name
			}
			out = append(out, l)
		}
	}
	return out, nil
}

func findSkills(dir string) (map[string][]string, error) {
	if _, err := os.Stat(dir); err != nil {
		return nil, err
	}
	found := map[string][]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() || path == dir {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") || d.Name() == "node_modules" {
			return filepath.SkipDir
		}
		if fi, err := os.Stat(filepath.Join(path, "SKILL.md")); err == nil && fi.Mode().IsRegular() {
			found[d.Name()] = append(found[d.Name()], path)
			return filepath.SkipDir
		}
		return nil
	})
	return found, err
}
