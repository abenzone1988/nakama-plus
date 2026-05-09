package template

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"

	"go.uber.org/zap"
)

func ReadTemplateFile(logger *zap.Logger, loadPath, filename string) []byte {
	path := filepath.Join(loadPath, filename)
	b, err := os.ReadFile(path)
	if err != nil {
		logger.Fatal("读取文件错误", zap.String("file", path), zap.Error(err))
		return nil
	}

	b = bytes.TrimSpace(b)
	if len(b) == 0 {
		logger.Fatal("模板文件为空", zap.String("file", path))
		return nil
	}
	if b[0] != '{' {
		logger.Fatal("模板 JSON 顶层必须为对象", zap.String("file", path))
		return nil
	}
	if !json.Valid(b) {
		logger.Fatal("模板 JSON 非法", zap.String("file", path))
		return nil
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		logger.Fatal("模板 JSON 解析失败", zap.String("file", path), zap.Error(err))
		return nil
	}
	if len(m) == 0 {
		logger.Fatal("模板 JSON 对象为空", zap.String("file", path))
		return nil
	}

	return b
}
