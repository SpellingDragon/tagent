package strictyaml

import (
	"strings"
	"testing"
)

type tinyCfg struct {
	Entry string `yaml:"entry"`
}

// TestDecodeYAML_RejectsUnknownField 钉住未知字段被拒且错误点名该字段（本文件是严格解码判据的执行体）。
//
// 契约: docs/wiki/platform/platform-subsystems.md#strict-decode
func TestDecodeYAML_RejectsUnknownField(t *testing.T) {
	err := DecodeYAML([]byte("entry: a\nwroking_dir: /tmp\n"), &tinyCfg{})
	if err == nil || !strings.Contains(err.Error(), "wroking_dir") {
		t.Fatalf("err = %v, want unknown-field error naming wroking_dir", err)
	}
}

func TestDecodeYAML_RejectsSecondDocument(t *testing.T) {
	err := DecodeYAML([]byte("entry: a\n---\nentry: b\n"), &tinyCfg{})
	if err == nil || !strings.Contains(err.Error(), "second document") {
		t.Fatalf("err = %v, want trailing-document rejection", err)
	}
}

func TestDecodeYAML_EmptyDocumentOK(t *testing.T) {
	if err := DecodeYAML([]byte(""), &tinyCfg{}); err != nil {
		t.Fatalf("empty doc must decode to zero value, got: %v", err)
	}
}

func TestDecodeJSON_RejectsTrailingContent(t *testing.T) {
	err := DecodeJSON([]byte(`{"entry":"a"} {"entry":"b"}`), &tinyCfg{})
	if err == nil || !strings.Contains(err.Error(), "unexpected content after the first value") {
		t.Fatalf("err = %v, want trailing-content rejection", err)
	}
}
