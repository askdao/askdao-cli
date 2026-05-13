// [INPUT]: 标准库 archive/zip / io/fs / os / path/filepath / bytes
// [OUTPUT]: ZipDir(srcDir, rootName) — 把一个 skill 目录打包成 zip（zip 内顶层目录 = rootName/）
// [POS]: internal/deploy 的 skill 目录打包器；cmd/askdao/deploy.go 上传 custom_local skill 前调用；产出形态对齐 conductor app/skills/sync.py 期望的「单一顶层目录 + 内含 SKILL.md」
// [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
package deploy

import (
	"archive/zip"
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
)

// ZipDir packages the contents of srcDir into an in-memory zip whose entries are
// rooted at rootName/ — e.g. ZipDir("/abs/path/my-skill", "my-skill") produces
// entries "my-skill/SKILL.md", "my-skill/scripts/run.py", ... This matches the
// "single top-level directory + SKILL.md inside it" layout the conductor's skill
// sync expects. Directory entries are implicit (the zip reader derives them from
// file paths); ".DS_Store" files are skipped.
func ZipDir(srcDir, rootName string) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	walkErr := filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if d.Name() == ".DS_Store" {
			return nil
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		w, err := zw.Create(rootName + "/" + filepath.ToSlash(rel))
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
