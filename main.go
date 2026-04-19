package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const (
	reset = "\033[0m"
	bold  = "\033[1m"
	dim   = "\033[2m"

	cyan   = "\033[36m"
	green  = "\033[32m"
	yellow = "\033[33m"
	red    = "\033[31m"
	white  = "\033[37m"
)

type Input struct {
	Model struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
	} `json:"model"`
	CWD       string `json:"cwd"`
	Version   string `json:"version"`
	Workspace struct {
		CurrentDir string `json:"current_dir"`
		ProjectDir string `json:"project_dir"`
	} `json:"workspace"`
	ContextWindow struct {
		TotalInputTokens  int     `json:"total_input_tokens"`
		TotalOutputTokens int     `json:"total_output_tokens"`
		ContextWindowSize int     `json:"context_window_size"`
		UsedPercentage    float64 `json:"used_percentage"`
		CurrentUsage      struct {
			InputTokens              int `json:"input_tokens"`
			OutputTokens             int `json:"output_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		} `json:"current_usage"`
	} `json:"context_window"`
	Cost struct {
		TotalCostUSD       float64 `json:"total_cost_usd"`
		TotalDurationMS    int64   `json:"total_duration_ms"`
		TotalAPIDurationMS int64   `json:"total_api_duration_ms"`
		TotalLinesAdded    int     `json:"total_lines_added"`
		TotalLinesRemoved  int     `json:"total_lines_removed"`
	} `json:"cost"`
	RateLimits struct {
		FiveHour struct {
			UsedPercentage float64 `json:"used_percentage"`
		} `json:"five_hour"`
		SevenDay struct {
			UsedPercentage float64 `json:"used_percentage"`
		} `json:"seven_day"`
	} `json:"rate_limits"`
}

func humanTokens(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.0fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.0fk", float64(n)/1_000)
	default:
		return strconv.Itoa(n)
	}
}

func humanDuration(ms int64) string {
	s := ms / 1000
	m := s / 60
	s = s % 60
	return fmt.Sprintf("%dm%02ds", m, s)
}

func repeat(s string, n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat(s, n)
}

type ThresholdConfig struct {
	Warning float64 `yaml:"warning"`
	Danger  float64 `yaml:"danger"`
}

func resolveThresholds(cfg Config, modelID string) (warn, danger float64) {
	warn, danger = 50, 75
	if cfg.Thresholds == nil {
		return
	}
	if d, ok := cfg.Thresholds["default"]; ok {
		if d.Warning > 0 {
			warn = d.Warning
		}
		if d.Danger > 0 {
			danger = d.Danger
		}
	}
	if t, ok := cfg.Thresholds[modelID]; ok {
		if t.Warning > 0 {
			warn = t.Warning
		}
		if t.Danger > 0 {
			danger = t.Danger
		}
	}
	return
}

func progressBar(pct float64, t ResolvedTheme, warn, danger float64) (bar, pctPart string) {
	const barWidth = 20
	filled := int(math.Round(pct * barWidth / 100))
	if filled > barWidth {
		filled = barWidth
	}

	greenEnd := int(math.Round(warn * float64(barWidth) / 100))
	yellowEnd := int(math.Round(danger * float64(barWidth) / 100))

	g := min(filled, greenEnd)
	y := 0
	if filled > greenEnd {
		y = min(filled, yellowEnd) - greenEnd
	}
	r := 0
	if filled > yellowEnd {
		r = filled - yellowEnd
	}

	emptyBeforeThresh := 0
	if filled < greenEnd {
		emptyBeforeThresh = greenEnd - filled
	}
	emptyAfterThresh := barWidth - max(filled, greenEnd)

	var b strings.Builder
	if g > 0 {
		b.WriteString(t.Success + repeat("█", g))
	}
	if emptyBeforeThresh > 0 {
		b.WriteString(dim + repeat("░", emptyBeforeThresh))
	}
	b.WriteString(t.Danger + "|" + reset)
	if y > 0 {
		b.WriteString(t.Warning + repeat("█", y))
	}
	if r > 0 {
		b.WriteString(t.Danger + repeat("█", r))
	}
	if emptyAfterThresh > 0 {
		b.WriteString(dim + repeat("░", emptyAfterThresh) + reset)
	}
	bar = b.String()

	var col string
	switch {
	case pct >= danger:
		col = t.Danger
	case pct >= warn:
		col = t.Warning
	default:
		col = t.Success
	}
	pctPart = fmt.Sprintf("%s%s%.0f%%%s", col, bold, pct, reset)
	return
}

func gitInfo(dir string) (branch string, added, modified, untracked int, hash string) {
	run := func(args ...string) (string, error) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		out, err := cmd.Output()
		return strings.TrimSpace(string(out)), err
	}

	if _, err := run("rev-parse", "--git-dir"); err != nil {
		return
	}

	branch, _ = run("symbolic-ref", "--short", "HEAD")
	if branch == "" {
		branch, _ = run("rev-parse", "--short", "HEAD")
	}

	out, _ := run("status", "--porcelain")
	for _, line := range strings.Split(out, "\n") {
		if len(line) < 2 {
			continue
		}
		xy := line[:2]
		if xy == "??" {
			untracked++
			continue
		}
		x, y := rune(xy[0]), rune(xy[1])
		if x != ' ' && x != '.' {
			added++
		}
		if y != ' ' && y != '.' {
			modified++
		}
	}

	hash, _ = run("rev-parse", "--short=8", "HEAD")
	return
}

func projectVersion(dirs ...string) string {
	reVersion := regexp.MustCompile(`"version"\s*:\s*"([^"]+)"`)
	reToml := regexp.MustCompile(`(?m)^version\s*=\s*"([^"]+)"`)

	for _, dir := range dirs {
		if data, err := os.ReadFile(filepath.Join(dir, "package.json")); err == nil {
			if m := reVersion.FindSubmatch(data); m != nil {
				return "v" + string(m[1])
			}
		}
		for _, f := range []string{"Cargo.toml", "pyproject.toml"} {
			if data, err := os.ReadFile(filepath.Join(dir, f)); err == nil {
				if m := reToml.FindSubmatch(data); m != nil {
					return "v" + string(m[1])
				}
			}
		}
	}
	return ""
}

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func visibleWidth(s string) int {
	return len(ansiRe.ReplaceAllString(s, ""))
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "init" {
		runInit()
		return
	}

	cfg := loadConfig()
	t := loadTheme(cfg.Theme)
	sep := dim + "|" + reset

	var buf bytes.Buffer
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		buf.Write(scanner.Bytes())
	}

	var in Input
	_ = json.Unmarshal(buf.Bytes(), &in)

	warn, danger := resolveThresholds(cfg, in.Model.ID)

	cwd := in.CWD
	if cwd == "" {
		cwd = in.Workspace.CurrentDir
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	projectDir := in.Workspace.ProjectDir
	if projectDir == "" {
		projectDir = cwd
	}

	// ── Render all blocks ─────────────────────────────────────────────────
	ctxSize := in.ContextWindow.ContextWindowSize
	ctxHuman := humanTokens(ctxSize)
	ctxInUse := in.ContextWindow.CurrentUsage.CacheReadInputTokens +
		in.ContextWindow.CurrentUsage.CacheCreationInputTokens +
		in.ContextWindow.CurrentUsage.InputTokens

	blocks := make(map[string]string)

	blocks["model"] = fmt.Sprintf("%s%s%s%s %s(%s context)%s",
		t.Primary, bold, in.Model.DisplayName, reset,
		dim, ctxHuman, reset)
	if ctxInUse > 0 {
		blocks["model"] += fmt.Sprintf(" %s[%s]%s", t.Text, humanTokens(ctxInUse), reset)
	}

	branch, gitAdded, gitModified, gitUntracked, hash := gitInfo(cwd)
	if branch != "" {
		counts := ""
		if gitAdded > 0 {
			counts += fmt.Sprintf("+%d ", gitAdded)
		}
		if gitModified > 0 {
			counts += fmt.Sprintf("~%d ", gitModified)
		}
		if gitUntracked > 0 {
			counts += fmt.Sprintf("?%d", gitUntracked)
		}
		counts = strings.TrimSpace(counts)
		if counts != "" {
			blocks["git"] = fmt.Sprintf("%s%s%s %s%s%s", t.Success, branch, reset, dim, counts, reset)
		} else {
			blocks["git"] = fmt.Sprintf("%s%s%s", t.Success, branch, reset)
		}
	}

	projectName := filepath.Base(projectDir)
	blocks["project"] = t.Text + projectName + reset

	version := in.Version
	if version == "" {
		version = projectVersion(cwd, projectDir)
	} else {
		version = "v" + version
	}
	if version != "" {
		blocks["version"] = dim + version + reset
	}

	bar, pctPart := progressBar(in.ContextWindow.UsedPercentage, t, warn, danger)
	blocks["bar"] = "[" + bar + "]"
	blocks["percent"] = pctPart

	costPart := dim + "$?" + reset
	if in.Cost.TotalCostUSD > 0 {
		c := in.Cost.TotalCostUSD
		if c < 0.01 {
			costPart = fmt.Sprintf("%s$%.4f%s", t.Text, c, reset)
		} else {
			costPart = fmt.Sprintf("%s$%.2f%s", t.Text, c, reset)
		}
	}
	blocks["cost"] = costPart

	if in.Cost.TotalDurationMS > 0 {
		elapsed := humanDuration(in.Cost.TotalDurationMS)
		if in.Cost.TotalAPIDurationMS > 0 {
			apiS := in.Cost.TotalAPIDurationMS / 1000
			blocks["time"] = fmt.Sprintf("%s%s (api:%ds)%s", dim, elapsed, apiS, reset)
		} else {
			blocks["time"] = dim + elapsed + reset
		}
	}

	cu := in.ContextWindow.CurrentUsage
	totalCur := cu.InputTokens + cu.CacheReadInputTokens + cu.CacheCreationInputTokens
	if totalCur > 0 {
		cachePct := float64(cu.CacheReadInputTokens) / float64(totalCur) * 100
		blocks["tokens"] = fmt.Sprintf("%s%s cache:%.0f%%%s", dim, humanTokens(totalCur), cachePct, reset)
	}

	fh := in.RateLimits.FiveHour.UsedPercentage
	sd := in.RateLimits.SevenDay.UsedPercentage
	if fh > 0 || sd > 0 {
		r := ""
		if fh > 0 {
			r += fmt.Sprintf("5h:%.0f%%", fh)
		}
		if sd > 0 {
			if r != "" {
				r += " "
			}
			r += fmt.Sprintf("7d:%.0f%%", sd)
		}
		blocks["rates"] = dim + r + reset
	}

	if in.Cost.TotalLinesAdded > 0 || in.Cost.TotalLinesRemoved > 0 {
		blocks["diff"] = fmt.Sprintf("%s+%d%s %s-%d%s",
			t.Success, in.Cost.TotalLinesAdded, reset,
			t.Danger, in.Cost.TotalLinesRemoved, reset)
	}

	if hash != "" {
		blocks["hash"] = dim + hash + reset
	}

	// ── Assemble lines ────────────────────────────────────────────────────
	tw := terminalWidth()
	sepLen := visibleWidth(" " + sep + " ")

	buildLine := func(order []string) string {
		var parts []string
		for _, name := range order {
			if s, ok := blocks[name]; ok {
				parts = append(parts, s)
			}
		}
		line := strings.Join(parts, " "+sep+" ")
		if visibleWidth(line) <= tw {
			return line
		}
		// Compact: keep only blocks from this line in compact priority order
		lineSet := make(map[string]bool)
		for _, name := range order {
			lineSet[name] = true
		}
		var compact []string
		for _, name := range cfg.Blocks.Compact {
			if lineSet[name] {
				if s, ok := blocks[name]; ok {
					compact = append(compact, s)
				}
			}
		}
		var fit []string
		w := 0
		for _, s := range compact {
			need := visibleWidth(s)
			if len(fit) > 0 {
				need += sepLen
			}
			if w+need > tw {
				break
			}
			fit = append(fit, s)
			w += need
		}
		if len(fit) == 0 && len(compact) > 0 {
			fit = compact[:1]
		}
		return strings.Join(fit, " "+sep+" ")
	}

	line1 := buildLine(cfg.Blocks.Line1)
	line2 := buildLine(cfg.Blocks.Line2)

	fmt.Printf("%s\n%s", line1, line2)
}
