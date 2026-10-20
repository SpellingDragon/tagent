package config

import (
	"errors"
	"strconv"
	"strings"
	"testing"
)

// TestDecodeStrictRefusesKeysOutsideTheCallerSchema pins the policy a host document inherits: a key the caller's schema does not name stops the decode.
// - an unknown root key, a key under a declared agent and a key under a free-form map entry are each refused by full field path
// - an unknown key inside a sequence item is refused by index
// - the refusal wraps ErrUnknownConfigKey, so a caller separates it from a value of the wrong shape
// - the same policy reads a json source
// 契约: docs/wiki/platform/platform-subsystems.md#strict-decode
func TestDecodeStrictRefusesKeysOutsideTheCallerSchema(t *testing.T) {
	cases := []struct {
		name     string
		file     string
		body     string
		field    string
		wantLine int
	}{
		{
			name:     "unknown root key",
			file:     "host.yaml",
			body:     "entry: bot\nlog_level: debug\n",
			field:    "log_level",
			wantLine: 2,
		},
		{
			name:     "unknown key under a free-form map entry",
			file:     "host.yaml",
			body:     "agents:\n  bot:\n    model: m\n    memory:\n      type: local\n",
			field:    "agents.bot.memory",
			wantLine: 4,
		},
		{
			name:     "unknown key inside a sequence item",
			file:     "host.yaml",
			body:     "agents:\n  bot:\n    tools:\n      - id: send\n        async: false\n",
			field:    "agents.bot.tools[0].async",
			wantLine: 5,
		},
		{
			name:     "unknown section",
			file:     "host.yaml",
			body:     "app:\n  wechat:\n    config_dir: d\n  slack:\n    token: t\n",
			field:    "app.slack",
			wantLine: 4,
		},
		{
			name:     "unknown json key",
			file:     "host.json",
			body:     "{\n  \"entry\": \"bot\",\n  \"trajectory_dump\": true\n}",
			field:    "trajectory_dump",
			wantLine: 3,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var doc hostSchema
			err := DecodeStrict(tc.file, []byte(tc.body), &doc)
			if err == nil {
				t.Fatalf("decode %s: accepted a key outside the schema: %+v", tc.file, doc)
			}
			if !errors.Is(err, ErrUnknownConfigKey) {
				t.Fatalf("decode %s: error %v does not wrap ErrUnknownConfigKey", tc.file, err)
			}
			for _, want := range []string{"config: " + tc.file + ":", tc.field + ": unknown field", "unknown field"} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("decode %s: error %q does not name %q", tc.file, err, want)
				}
			}
			if want := ":" + strconv.Itoa(tc.wantLine) + ":"; !strings.Contains(err.Error(), want) {
				t.Fatalf("decode %s: error %q does not name line %d", tc.file, err, tc.wantLine)
			}
		})
	}
}

// TestDecodeStrictReadsTheSchemaItNames pins the accepted side: a document the schema names decodes into the caller's struct, free-form map keys included.
// - every named section lands in the target field, map keys of the caller's choosing included
// - the zero document decodes into the zero value
// - a value of the wrong shape is refused by source without the key sentinel
// - a target that states no schema is refused by its own shape
// 契约: docs/wiki/platform/platform-subsystems.md#strict-decode
func TestDecodeStrictReadsTheSchemaItNames(t *testing.T) {
	body := `entry: bot
provider: zhipu
request_timeout_seconds: 3600
providers:
  zhipu: { provider: openai, api_endpoint: "https://example/v1", api_key_env: KEY }
  any_other_name: { provider: openai, api_key_env: OTHER_KEY }
agents:
  bot:
    system_prompt:
      files: [AGENTS.md, SOUL.md]
    tools:
      - { kind: tool, id: send }
app:
  wechat:
    config_dir: .wechat-config
`
	var doc hostSchema
	if err := DecodeStrict("host.yaml", []byte(body), &doc); err != nil {
		t.Fatalf("decode host.yaml: %v", err)
	}
	if doc.Entry != "bot" || doc.Provider != "zhipu" || doc.TimeoutSeconds != 3600 {
		t.Fatalf("decoded header = %+v, want entry bot, provider zhipu, timeout 3600", doc)
	}
	if got := doc.Providers["any_other_name"].APIKeyEnv; got != "OTHER_KEY" {
		t.Fatalf("free-form provider key decoded to api_key_env %q, want OTHER_KEY", got)
	}
	if files := doc.Agents["bot"].SystemPrompt.Files; len(files) != 2 || files[1] != "SOUL.md" {
		t.Fatalf("decoded prompt files = %v, want [AGENTS.md SOUL.md]", files)
	}
	if tools := doc.Agents["bot"].Tools; len(tools) != 1 || tools[0].ID != "send" {
		t.Fatalf("decoded tools = %v, want one send reference", tools)
	}
	if doc.App.Wechat.ConfigDir != ".wechat-config" {
		t.Fatalf("decoded app.wechat = %+v, want the named config dir", doc.App.Wechat)
	}

	var empty hostSchema
	if err := DecodeStrict("empty.yaml", nil, &empty); err != nil {
		t.Fatalf("decode empty document: %v", err)
	}
	if empty.Entry != "" || len(empty.Agents) != 0 {
		t.Fatalf("empty document decoded to %+v, want the zero schema", empty)
	}

	err := DecodeStrict("shape.yaml", []byte("request_timeout_seconds: soon\n"), &hostSchema{})
	if err == nil || errors.Is(err, ErrUnknownConfigKey) {
		t.Fatalf("wrong shape error = %v, want a shape rejection that carries no key sentinel", err)
	}

	err = DecodeStrict("target.yaml", []byte("entry: bot\n"), "not a struct")
	if err == nil || !strings.Contains(err.Error(), "pointer to a struct") {
		t.Fatalf("refused target error = %v, want the target shape named", err)
	}
}

// hostSchema is a caller-owned document schema: the fixture stands for a host
// document whose sections this package does not itself declare.
type hostSchema struct {
	Entry          string                   `yaml:"entry"`
	Provider       string                   `yaml:"provider"`
	TimeoutSeconds int                      `yaml:"request_timeout_seconds"`
	Providers      map[string]providerShape `yaml:"providers"`
	Agents         map[string]agentShape    `yaml:"agents"`
	App            struct {
		Wechat wechatShape `yaml:"wechat"`
	} `yaml:"app"`
}

// providerShape is one connection registry entry of the fixture.
type providerShape struct {
	Provider    string `yaml:"provider"`
	APIEndpoint string `yaml:"api_endpoint"`
	APIKeyEnv   string `yaml:"api_key_env"`
}

// agentShape is one declared agent of the fixture.
type agentShape struct {
	Model        string `yaml:"model"`
	SystemPrompt struct {
		Files []string `yaml:"files"`
	} `yaml:"system_prompt"`
	Tools []toolShape `yaml:"tools"`
}

// toolShape is one tool reference of the fixture.
type toolShape struct {
	Kind string `yaml:"kind"`
	ID   string `yaml:"id"`
}

// wechatShape is the application section of the fixture.
type wechatShape struct {
	ConfigDir string `yaml:"config_dir"`
}
