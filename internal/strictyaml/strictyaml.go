// Package strictyaml 是所有配置入口共用的**唯一**严格解码实现：未知字段一律让加载明确
// 失败，而不是静默忽略一个拼错的关键字。一处实现、多处调用——新增配置入口必须走本包，
// 不得另立第二套严格度。
//
// 契约: docs/wiki/platform/platform-subsystems.md#strict-decode
package strictyaml

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"gopkg.in/yaml.v3"
)

// DecodeYAML 严格解析 YAML：拒绝未知字段，并拒绝首个文档之后的任何尾随文档——静默忽略
// 第二个文档会让用户以为其中的配置已经生效。
func DecodeYAML(data []byte, out any) error {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(out); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return fmt.Errorf("strict yaml: %w", err)
	}
	var extra yaml.Node
	switch err := dec.Decode(&extra); {
	case errors.Is(err, io.EOF):
		return nil
	case err != nil:
		return fmt.Errorf("strict yaml: trailing content: %w", err)
	default:
		return fmt.Errorf("strict yaml: unexpected second document after the first (multi-document streams are not config)")
	}
}

// DecodeJSON 严格解析 JSON：拒绝未知字段，并拒绝首个值之后的任何尾随内容。
func DecodeJSON(data []byte, out any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return fmt.Errorf("strict json: %w", err)
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return fmt.Errorf("strict json: trailing content: %w", err)
		}
		return fmt.Errorf("strict json: unexpected content after the first value")
	}
	return nil
}

// DecodeByExt 按扩展名分派（.yaml/.yml 走 YAML，其余走 JSON），与 LoadConfig 的格式自动识别
// 保持一致。
func DecodeByExt(path string, data []byte, out any) error {
	if strings.HasSuffix(strings.ToLower(path), ".yaml") || strings.HasSuffix(strings.ToLower(path), ".yml") {
		return DecodeYAML(data, out)
	}
	return DecodeJSON(data, out)
}
