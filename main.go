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

// ANSI style modifiers (not colors — not theme-controlled)
const (
	reset = "\033[0m"
	bold  = "\033[1m"
	dim   = "\033[2m"

	// named ANSI colors kept for resolveColor mapping
	cyan   = "\033[36m"
	green  = "\033[32m"
	yellow = "\033[33m"
	red    = "\033[31m"
	white  = "\033[37m"
)

type Input struct {
	Model struct {
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

func progressBar(pct float64, t ResolvedTheme) (bar, pctPart string) {
	const barWidth = 20
	filled := int(math.Round(pct * barWidth / 100))
	if filled > barWidth {
		filled = barWidth
	}

	greenEnd := barWidth * 50 / 100  // 10
	yellowEnd := barWidth * 75 / 100 // 15

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
	case pct >= 75:
		col = t.Danger
	case pct >= 50:
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

	// ── Line 1 ────────────────────────────────────────────────────────────────
	ctxSize := in.ContextWindow.ContextWindowSize
	ctxHuman := humanTokens(ctxSize)
	ctxInUse := in.ContextWindow.CurrentUsage.CacheReadInputTokens +
		in.ContextWindow.CurrentUsage.CacheCreationInputTokens +
		in.ContextWindow.CurrentUsage.InputTokens

	modelPart := fmt.Sprintf("%s%s%s%s %s(%s context)%s",
		t.Primary, bold, in.Model.DisplayName, reset,
		dim, ctxHuman, reset)
	if ctxInUse > 0 {
		modelPart += fmt.Sprintf(" %s[%s]%s", t.Text, humanTokens(ctxInUse), reset)
	}

	branch, gitAdded, gitModified, gitUntracked, hash := gitInfo(cwd)
	gitPart := ""
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
			gitPart = fmt.Sprintf("%s%s%s %s%s%s", t.Success, branch, reset, dim, counts, reset)
		} else {
			gitPart = fmt.Sprintf("%s%s%s", t.Success, branch, reset)
		}
	}

	projectName := filepath.Base(projectDir)
	projectPart := t.Text + projectName + reset

	version := in.Version
	if version == "" {
		version = projectVersion(cwd, projectDir)
	} else {
		version = "v" + version
	}
	versionPart := ""
	if version != "" {
		versionPart = dim + version + reset
	}

	parts1 := []string{modelPart}
	if gitPart != "" {
		parts1 = append(parts1, gitPart)
	}
	parts1 = append(parts1, projectPart)
	if versionPart != "" {
		parts1 = append(parts1, versionPart)
	}
	line1 := strings.Join(parts1, " "+sep+" ")

	// ── Line 2 ────────────────────────────────────────────────────────────────
	bar, pctPart := progressBar(in.ContextWindow.UsedPercentage, t)

	costPart := dim + "$?" + reset
	if in.Cost.TotalCostUSD > 0 {
		c := in.Cost.TotalCostUSD
		if c < 0.01 {
			costPart = fmt.Sprintf("%s$%.4f%s", t.Text, c, reset)
		} else {
			costPart = fmt.Sprintf("%s$%.2f%s", t.Text, c, reset)
		}
	}

	timePart := ""
	if in.Cost.TotalDurationMS > 0 {
		elapsed := humanDuration(in.Cost.TotalDurationMS)
		if in.Cost.TotalAPIDurationMS > 0 {
			apiS := in.Cost.TotalAPIDurationMS / 1000
			timePart = fmt.Sprintf("%s%s (api:%ds)%s", dim, elapsed, apiS, reset)
		} else {
			timePart = dim + elapsed + reset
		}
	}

	tokenPart := ""
	cu := in.ContextWindow.CurrentUsage
	totalCur := cu.InputTokens + cu.CacheReadInputTokens + cu.CacheCreationInputTokens
	if totalCur > 0 {
		cachePct := 0.0
		if totalCur > 0 {
			cachePct = float64(cu.CacheReadInputTokens) / float64(totalCur) * 100
		}
		tokenPart = fmt.Sprintf("%s%s cache:%.0f%%%s", dim, humanTokens(totalCur), cachePct, reset)
	}

	ratePart := ""
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
		ratePart = dim + r + reset
	}

	diffPart := ""
	if in.Cost.TotalLinesAdded > 0 || in.Cost.TotalLinesRemoved > 0 {
		diffPart = fmt.Sprintf("%s+%d%s %s-%d%s",
			t.Success, in.Cost.TotalLinesAdded, reset,
			t.Danger, in.Cost.TotalLinesRemoved, reset)
	}

	hashPart := ""
	if hash != "" {
		hashPart = dim + hash + reset
	}

	parts2 := []string{"[" + bar + "]", pctPart, costPart}
	if timePart != "" {
		parts2 = append(parts2, timePart)
	}
	if tokenPart != "" {
		parts2 = append(parts2, tokenPart)
	}
	if ratePart != "" {
		parts2 = append(parts2, ratePart)
	}
	if diffPart != "" {
		parts2 = append(parts2, diffPart)
	}
	if hashPart != "" {
		parts2 = append(parts2, hashPart)
	}
	line2 := strings.Join(parts2, " "+sep+" ")

	fmt.Printf("%s\n%s", line1, line2)
}

