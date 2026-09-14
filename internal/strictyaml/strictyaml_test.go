package strictyaml

import (
	"strings"
	"testing"
)

type tinyCfg struct {
	Entry string `yaml:"entry"`
}

func TestDecodeYAML_RejectsUnknownField(t *testing.T) {
	err := DecodeYAML([]byte("entry: a\nwroking_dir: /tmp\n"), &tinyCfg{})
	if err == nil || !strings.Contains(err.Error(), "wroking_dir") {
		t.Fatalf("err = %v, want unknown-field error naming wroking_dir", err)
	}
}

func TestDecodeYAML_RejectsSecondDocument(t *testing.T) {
	// Review P2-4: the old single-Decode silently ignored everything after
	// the first `---` — a second document could carry config the user
	// believes is live.
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
