// [INPUT]: 标准库 encoding/json / fmt / os / path/filepath / strings
// [OUTPUT]: 对外提供 Install / SweepStale
// [POS]: observe 模块的唯一实现 —— 管理临时项目级 .claude/settings.local.json 的写入/合并/清理；
//
//	被 cmd/askdao/edit.go 的 --observe 流程消费：Install 写 PreToolUse hook(指向 webstudio /api/observe)，
//	cleanup 字节级还原，SweepStale 启动自检清残留。零残留三件套之二（整文件备份还原 + 启动自检）。
//
// [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
package observe

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// observePath is both the receiver route and the marker identifying the hook
// rules we append — cleanup/sweep match on a URL containing this path.
const observePath = "/api/observe"

func settingsFile(projectRoot string) string {
	return filepath.Join(projectRoot, ".claude", "settings.local.json")
}

// Install writes a temporary project-level .claude/settings.local.json with two
// PreToolUse hook rules (matcher "Skill" + "mcp__.*") POSTing to the webstudio
// observe endpoint on port. Existing user settings are preserved — we only append
// to hooks.PreToolUse, never rewrite other keys. The returned cleanup restores the
// exact prior state: byte-for-byte if the file existed, or removes it if we made it.
//
// Caller MUST defer cleanup. As a backstop against a killed process where defer
// never runs, call SweepStale at startup — defer alone is not enough (spike R6).
func Install(projectRoot string, port int) (cleanup func() error, err error) {
	path := settingsFile(projectRoot)

	original, readErr := os.ReadFile(path)
	existed := readErr == nil
	if readErr != nil && !os.IsNotExist(readErr) {
		return nil, fmt.Errorf("observe: read %s: %w", path, readErr)
	}

	settings := map[string]interface{}{}
	if existed {
		if err := json.Unmarshal(original, &settings); err != nil {
			return nil, fmt.Errorf("observe: %s is not valid JSON (fix or remove it): %w", path, err)
		}
	}

	// Idempotent: drop any stale observe rules, then append fresh ones bound to port.
	pre := stripObserveRules(preToolUse(settings))
	pre = append(pre, rule("Skill", port), rule("mcp__.*", port))
	setPreToolUse(settings, pre)

	if err := writeSettings(path, settings); err != nil {
		return nil, err
	}

	cleanup = func() error {
		if existed {
			return os.WriteFile(path, original, 0o644)
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return cleanup, nil
}

// SweepStale removes leftover observe hook rules from a previous run whose cleanup
// never ran (process killed / Ctrl-C). Safe and cheap to call on every start; a
// no-op when the file is absent, malformed, or has no observe rules.
func SweepStale(projectRoot string) error {
	path := settingsFile(projectRoot)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	settings := map[string]interface{}{}
	if err := json.Unmarshal(raw, &settings); err != nil {
		return nil // leave a user-malformed file untouched
	}
	pre := preToolUse(settings)
	cleaned := stripObserveRules(pre)
	if len(cleaned) == len(pre) {
		return nil // nothing stale
	}
	setPreToolUse(settings, cleaned)
	return writeSettings(path, settings)
}

// rule builds one PreToolUse entry: matcher -> HTTP POST to the observe endpoint.
func rule(matcher string, port int) map[string]interface{} {
	return map[string]interface{}{
		"matcher": matcher,
		"hooks": []interface{}{
			map[string]interface{}{
				"type":    "http",
				"url":     fmt.Sprintf("http://127.0.0.1:%d%s", port, observePath),
				"timeout": 10,
			},
		},
	}
}

func preToolUse(settings map[string]interface{}) []interface{} {
	hooks, ok := settings["hooks"].(map[string]interface{})
	if !ok {
		return nil
	}
	pre, _ := hooks["PreToolUse"].([]interface{})
	return pre
}

// setPreToolUse writes pre back, pruning empty hooks/PreToolUse containers so we
// don't leave {"hooks":{"PreToolUse":[]}} noise behind after a sweep.
func setPreToolUse(settings map[string]interface{}, pre []interface{}) {
	hooks, _ := settings["hooks"].(map[string]interface{})
	if len(pre) == 0 {
		if hooks != nil {
			delete(hooks, "PreToolUse")
			if len(hooks) == 0 {
				delete(settings, "hooks")
			}
		}
		return
	}
	if hooks == nil {
		hooks = map[string]interface{}{}
		settings["hooks"] = hooks
	}
	hooks["PreToolUse"] = pre
}

func stripObserveRules(pre []interface{}) []interface{} {
	out := make([]interface{}, 0, len(pre))
	for _, item := range pre {
		if !isObserveRule(item) {
			out = append(out, item)
		}
	}
	return out
}

// isObserveRule reports whether a PreToolUse entry is one we appended — any of its
// hooks targets a URL containing /api/observe.
func isObserveRule(item interface{}) bool {
	m, ok := item.(map[string]interface{})
	if !ok {
		return false
	}
	hooks, ok := m["hooks"].([]interface{})
	if !ok {
		return false
	}
	for _, h := range hooks {
		hm, ok := h.(map[string]interface{})
		if !ok {
			continue
		}
		if url, ok := hm["url"].(string); ok && strings.Contains(url, observePath) {
			return true
		}
	}
	return false
}

func writeSettings(path string, settings map[string]interface{}) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("observe: mkdir %s: %w", filepath.Dir(path), err)
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("observe: marshal settings: %w", err)
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
