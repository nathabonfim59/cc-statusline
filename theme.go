package main

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed themes/*.yaml
var builtinThemesFS embed.FS

//go:embed config.example.yaml
var sampleConfig []byte

type ThemeColors struct {
	Primary string `yaml:"primary"`
	Text    string `yaml:"text"`
	Success string `yaml:"success"`
	Warning string `yaml:"warning"`
	Danger  string `yaml:"danger"`
}

type ThemeFile struct {
	Name   string      `yaml:"name"`
	Colors ThemeColors `yaml:"colors"`
}

// ResolvedTheme holds ready-to-use ANSI escape sequences.
type ResolvedTheme struct {
	Primary string
	Text    string
	Success string
	Warning string
	Danger  string
}

type BlockConfig struct {
	Line1   []string `yaml:"line1"`
	Line2   []string `yaml:"line2"`
	Compact []string `yaml:"compact"`
}

type Config struct {
	Theme      string                     `yaml:"theme"`
	Thresholds map[string]ThresholdConfig `yaml:"thresholds"`
	Blocks     BlockConfig                `yaml:"blocks"`
}

var builtinDefault = ThemeFile{
	Name: "Default",
	Colors: ThemeColors{
		Primary: "cyan",
		Text:    "white",
		Success: "green",
		Warning: "yellow",
		Danger:  "red",
	},
}

func configDir() string {
	if runtime.GOOS == "windows" {
		if dir, err := os.UserConfigDir(); err == nil {
			return filepath.Join(dir, "claude-statusline")
		}
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "claude-statusline")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "claude-statusline")
}

func runInit() {
	dir := configDir()
	themesDir := filepath.Join(dir, "themes")
	if err := os.MkdirAll(themesDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "error creating config directory: %v\n", err)
		os.Exit(1)
	}

	cfgPath := filepath.Join(dir, "config.yaml")
	if _, err := os.Stat(cfgPath); err == nil {
		fmt.Printf("config already exists at %s\n", cfgPath)
		return
	}

	if err := os.WriteFile(cfgPath, sampleConfig, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "error writing config: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("created config at %s\n", cfgPath)
}

func loadConfig() Config {
	cfg := Config{
		Theme: "default",
		Blocks: BlockConfig{
			Line1:   []string{"model", "git", "project", "version"},
			Line2:   []string{"bar", "percent", "cost", "time", "tokens", "rates", "diff", "hash"},
			Compact: []string{"model", "bar", "percent", "cost", "git", "project", "hash", "time", "tokens", "rates", "diff", "version"},
		},
	}
	data, err := os.ReadFile(filepath.Join(configDir(), "config.yaml"))
	if err != nil {
		return cfg
	}
	_ = yaml.Unmarshal(data, &cfg)
	if len(cfg.Blocks.Line1) == 0 {
		cfg.Blocks.Line1 = []string{"model", "git", "project", "version"}
	}
	if len(cfg.Blocks.Line2) == 0 {
		cfg.Blocks.Line2 = []string{"bar", "percent", "cost", "time", "tokens", "rates", "diff", "hash"}
	}
	if len(cfg.Blocks.Compact) == 0 {
		cfg.Blocks.Compact = []string{"model", "bar", "percent", "cost", "git", "project", "hash", "time", "tokens", "rates", "diff", "version"}
	}
	return cfg
}

func resolveColor(val string) string {
	switch strings.ToLower(strings.TrimSpace(val)) {
	case "cyan":
		return cyan
	case "green":
		return green
	case "yellow":
		return yellow
	case "red":
		return red
	case "white":
		return white
	case "dim":
		return dim
	case "bold":
		return bold
	case "reset", "default", "":
		return reset
	}
	if strings.HasPrefix(val, "#") && len(val) == 7 {
		r := hexNibble(val[1:3])
		g := hexNibble(val[3:5])
		b := hexNibble(val[5:7])
		return fmt.Sprintf("\033[38;2;%d;%d;%dm", r, g, b)
	}
	return val // pass through raw ANSI sequences
}

func hexNibble(s string) int {
	v := 0
	for _, c := range strings.ToLower(s) {
		v <<= 4
		switch {
		case c >= '0' && c <= '9':
			v |= int(c - '0')
		case c >= 'a' && c <= 'f':
			v |= int(c-'a') + 10
		}
	}
	return v
}

func resolveTheme(tf ThemeFile) ResolvedTheme {
	return ResolvedTheme{
		Primary: resolveColor(tf.Colors.Primary),
		Text:    resolveColor(tf.Colors.Text),
		Success: resolveColor(tf.Colors.Success),
		Warning: resolveColor(tf.Colors.Warning),
		Danger:  resolveColor(tf.Colors.Danger),
	}
}

// loadTheme resolves a theme by name.
// Precedence: local override (~/.config/claude-statusline/themes/<name>.yaml)
// → built-in (embedded themes/<name>.yaml) → hard-coded default.
func loadTheme(name string) ResolvedTheme {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		name = "default"
	}

	// 1. Local override
	localPath := filepath.Join(configDir(), "themes", name+".yaml")
	if data, err := os.ReadFile(localPath); err == nil {
		var tf ThemeFile
		if yaml.Unmarshal(data, &tf) == nil && tf.Colors.Primary != "" {
			return resolveTheme(tf)
		}
	}

	// 2. Built-in embedded theme
	if data, err := builtinThemesFS.ReadFile("themes/" + name + ".yaml"); err == nil {
		var tf ThemeFile
		if yaml.Unmarshal(data, &tf) == nil {
			return resolveTheme(tf)
		}
	}

	// 3. Hard-coded fallback
	return resolveTheme(builtinDefault)
}
