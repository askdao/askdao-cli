// [INPUT]: 标准库 archive/zip / bufio / bytes / io/fs / os / path/filepath / strings
// [OUTPUT]: ZipDir(srcDir, rootName) — 把一个 skill 目录打包成 zip（zip 内顶层目录 = rootName/），
//
//	带默认 ignore 过滤（VCS/依赖/虚拟环境/编辑器&OS 垃圾 + 安全关键的 dotenv）+ 可选 .askdaoignore 兜底
//
// [POS]: internal/deploy 的 skill 目录打包器；cmd/askdao/deploy.go packageSkills 上传 custom_local skill 前调用；
//
//	产出形态对齐服务端期望的「单一顶层目录 + 内含 SKILL.md」。
//	ignore 过滤同时保护两个下游：① 服务端落库的真源 blob ② 上传 Anthropic Managed Skills 的文件列表——
//	二者都从这同一份 zip 派生，故在打包处一刀过滤即全覆盖，杜绝 .env/密钥/node_modules 误传。
//
// [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
package deploy

import (
	"archive/zip"
	"bufio"
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// excludedDirs 是打包时整棵跳过的目录名（filepath.SkipDir）：VCS 内部 / 依赖 / 虚拟环境 /
// 缓存 —— 它们从不属于 skill 包，且可能体积巨大或含敏感历史。只用精确名匹配，不用前缀通配
// （避免误伤 KOL 合法命名的 skill 子目录，如 output_templates / inputs_guide）；项目特有的
// 构建输出目录由 .askdaoignore 兜底排除。
var excludedDirs = map[string]bool{
	"node_modules": true,
	".git":         true,
	".svn":         true,
	".hg":          true,
	"__pycache__":  true,
	".venv":        true,
	"venv":         true,
}

// isExcludedFile 报告一个文件是否按默认规则跳过：OS/编辑器垃圾，以及（安全关键）dotenv
// 文件——skill 是 prompt/指令包，几乎不需要 .env，默认排除以杜绝密钥误传；个别确需的模板
// （如 .env.example）可用 .askdaoignore 的 "!" 反向纳入。.askdaoignore 自身也不打包。
func isExcludedFile(name string) bool {
	switch name {
	case ".DS_Store", "Thumbs.db", "desktop.ini", ".askdaoignore":
		return true
	}
	if name == ".env" || strings.HasPrefix(name, ".env.") {
		return true
	}
	if strings.HasSuffix(name, "~") ||
		strings.HasSuffix(name, ".swp") ||
		strings.HasSuffix(name, ".swo") {
		return true
	}
	return false
}

// ignoreRule 是一条 .askdaoignore 规则。negate=true 对应 "!pattern"（反向纳入）。
type ignoreRule struct {
	pattern string
	negate  bool
}

// loadAskdaoIgnore 读取 <srcDir>/.askdaoignore（gitignore 风格：# 注释、空行跳过、前导 "!"
// 反向纳入、尾随 "/" 视为目录模式）。文件不存在 → nil（无规则）。
func loadAskdaoIgnore(srcDir string) []ignoreRule {
	f, err := os.Open(filepath.Join(srcDir, ".askdaoignore"))
	if err != nil {
		return nil
	}
	defer f.Close()

	var rules []ignoreRule
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		neg := false
		if strings.HasPrefix(line, "!") {
			neg = true
			line = strings.TrimSpace(line[1:])
		}
		line = strings.TrimSuffix(line, "/")
		if line == "" {
			continue
		}
		rules = append(rules, ignoreRule{pattern: line, negate: neg})
	}
	return rules
}

// ruleMatches 判断一条模式是否命中 slash 形式的相对路径 rel：命中任一路径段、对全路径或
// basename 的 filepath.Match 成功、或作为目录前缀（pattern/...）。
func ruleMatches(pattern, rel, base string, segs []string) bool {
	for _, s := range segs {
		if s == pattern {
			return true
		}
	}
	if ok, _ := filepath.Match(pattern, rel); ok {
		return true
	}
	if ok, _ := filepath.Match(pattern, base); ok {
		return true
	}
	return strings.HasPrefix(rel, pattern+"/")
}

// askdaoIgnored 对 rel 路径按 rules 顺序求值（gitignore 语义：后者覆盖前者；命中 "!" 规则
// 则重新纳入）。无规则 → false。
func askdaoIgnored(rel string, rules []ignoreRule) bool {
	if len(rules) == 0 {
		return false
	}
	segs := strings.Split(rel, "/")
	base := segs[len(segs)-1]
	ignored := false
	for _, r := range rules {
		if ruleMatches(r.pattern, rel, base, segs) {
			ignored = !r.negate
		}
	}
	return ignored
}

// ZipDir packages the contents of srcDir into an in-memory zip whose entries are
// rooted at rootName/ — e.g. ZipDir("/abs/path/my-skill", "my-skill") produces
// entries "my-skill/SKILL.md", "my-skill/scripts/run.py", ... This matches the
// "single top-level directory + SKILL.md inside it" layout the conductor's skill
// sync expects. Directory entries are implicit (the zip reader derives them from
// file paths).
//
// Files/dirs are filtered by isExcludedFile / excludedDirs (default-deny for
// VCS/deps/venv/editor-OS junk + security-critical dotenv) plus an optional
// per-skill .askdaoignore. The root SKILL.md is always kept (a skill is invalid
// without it).
func ZipDir(srcDir, rootName string) ([]byte, error) {
	rules := loadAskdaoIgnore(srcDir)

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	walkErr := filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(srcDir, path)
		if relErr != nil {
			return relErr
		}
		relSlash := filepath.ToSlash(rel)

		if d.IsDir() {
			if rel == "." {
				return nil
			}
			if excludedDirs[d.Name()] || askdaoIgnored(relSlash, rules) {
				return filepath.SkipDir
			}
			return nil
		}

		// 硬保留根 SKILL.md（skill 缺它即非法），其余文件过 ignore 过滤
		if relSlash != "SKILL.md" {
			if isExcludedFile(d.Name()) || askdaoIgnored(relSlash, rules) {
				return nil
			}
		}

		w, err := zw.Create(rootName + "/" + relSlash)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	})
	if walkErr != nil {
		_ = zw.Close()
		return nil, walkErr
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
