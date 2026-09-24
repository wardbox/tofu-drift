// Package config reads tofu-drift.toml: the Ignore Rules.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/wardbox/tofu-drift/internal/scan"
)

type Config struct {
	Ignore []Rule `toml:"ignore"`
}

// Rule is one Ignore Rule. Every field set must match; an unset field matches
// anything.
type Rule struct {
	// Tag is "Key=Value".
	Tag  string `toml:"tag"`
	Type string `toml:"type"`
	// ARN is a path.Match glob; * does not cross a /.
	ARN string `toml:"arn"`
}

// Load parses the config at p. A missing file is an empty Config unless
// required.
func Load(p string, required bool) (Config, error) {
	var c Config
	md, err := toml.DecodeFile(p, &c)
	if errors.Is(err, fs.ErrNotExist) && !required {
		return Config{}, nil
	}
	if err != nil {
		return c, fmt.Errorf("config %s: %w", p, err)
	}
	if u := md.Undecoded(); len(u) > 0 {
		return c, fmt.Errorf("config %s: unknown key %s", p, u[0])
	}
	for i, r := range c.Ignore {
		if r == (Rule{}) {
			return c, fmt.Errorf("config %s: ignore rule %d sets none of tag, type, arn", p, i+1)
		}
		if r.Tag != "" && !strings.Contains(r.Tag, "=") {
			return c, fmt.Errorf("config %s: ignore rule %d: tag %q is not Key=Value", p, i+1, r.Tag)
		}
		if _, err := path.Match(r.ARN, ""); err != nil {
			return c, fmt.Errorf("config %s: ignore rule %d: arn %q: %w", p, i+1, r.ARN, err)
		}
	}
	return c, nil
}

// Ignored reports whether any Ignore Rule matches l.
func (c Config) Ignored(l scan.LiveResource) bool {
	for _, r := range c.Ignore {
		if r.matches(l) {
			return true
		}
	}
	return false
}

func (r Rule) matches(l scan.LiveResource) bool {
	if r.Type != "" && r.Type != l.Type {
		return false
	}
	if r.Tag != "" {
		k, v, _ := strings.Cut(r.Tag, "=")
		if got, ok := l.Tags[k]; !ok || got != v {
			return false
		}
	}
	if r.ARN != "" {
		if ok, _ := path.Match(r.ARN, l.ARN); !ok || l.ARN == "" {
			return false
		}
	}
	return true
}
