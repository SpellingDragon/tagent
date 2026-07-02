// Copyright 2025 tagent authors. All rights reserved.
// Use of this source code is governed by a BSD-style license.

// testing.go provides exported helpers for integration tests in tests/.
// These expose internal APIs for comprehensive testing. Do NOT rely on
// them in production code — they may change without notice.
//
// Convention: all symbols use the "Testing" prefix.
package tagent

import (
	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/prompt"
	tagenttool "github.com/SpellingDragon/tagent/tool"
	"trpc.group/trpc-go/trpc-agent-go/model"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// TestingBuildAgent creates a TagentAgent using the internal build pipeline.
// Test-only — do NOT use in production code.
func TestingBuildAgent(
	name string,
	acfg AgentConfig,
	cfg Config,
	m model.Model,
	skillRepo tagenttool.SkillRepository,
	mcpToolSets []trpctool.ToolSet,
	loader *prompt.Loader,
	cache map[string]*agent.TagentAgent,
) (*agent.TagentAgent, error) {
	rc := &runtimeConfig{
		model:       m,
		skillRepo:   skillRepo,
		mcpToolSets: mcpToolSets,
	}
	return buildAgent(name, acfg, cfg, rc, loader, cache)
}
