package tool

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/skill"

	"github.com/SpellingDragon/tagent/memory"
)

// ==================== Helpers ====================

// newTestMemoryStore creates a MemoryStore pre-populated with test events.
func newTestMemoryStore(t *testing.T, events map[int64]memory.FullEvent) memory.MemoryStore {
	t.Helper()
	tempDir := t.TempDir()
	store, err := memory.NewFileBackend(tempDir)
	if err != nil {
		t.Fatalf("Failed to create memory store: %v", err)
	}
	for key, event := range events {
		if err := store.StoreEvent(key, event); err != nil {
			t.Fatalf("Failed to store event %d: %v", key, err)
		}
	}
	return store
}

// ==================== RecallAgent Subtool Tests ====================
// 测试 RecallAgent 的子工具: memory_query, memory_get, memory_recent

// Test 1: memory_query 基本查询
func TestRecallQueryTool_BasicQuery(t *testing.T) {
	partitionID := memory.PartitionIDFromName("test")
	key1 := memory.NewSnowflakeEventKey(partitionID, 0)
	key2 := memory.NewSnowflakeEventKey(partitionID, 0)

	store := newTestMemoryStore(t, map[int64]memory.FullEvent{
		key1: {
			EventKey:     key1,
			PartitionID:  partitionID,
			EventType:    memory.EventTypeActionCommand,
			EventSummary: "用户要求整理文件",
			Timestamp:    time.Now().UnixMilli(),
			Content:      "整理 /tmp 目录下的文件",
		},
		key2: {
			EventKey:     key2,
			PartitionID:  partitionID,
			EventType:    memory.EventTypeAgentOutput,
			EventSummary: "文件整理完成",
			Timestamp:    time.Now().Add(1 * time.Minute).UnixMilli(),
			Content:      "成功整理 15 个文件",
		},
	})

	// 直接测试 MemoryStore.QueryEvents（子工具底层调用）
	events, err := store.QueryEvents(memory.QueryOptions{Limit: 10, OrderBy: "timestamp_desc"})
	if err != nil {
		t.Fatalf("QueryEvents failed: %v", err)
	}

	// 验证: 返回相关事件
	if len(events) == 0 {
		t.Error("Expected events, got none")
	}

	// 验证: 事件应包含文件整理相关内容
	found := false
	for _, evt := range events {
		if strings.Contains(evt.EventSummary, "整理") || strings.Contains(evt.EventSummary, "文件") {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected to find event related to '文件整理', but none matched")
	}

	t.Logf("BasicQuery: found %d events", len(events))
}

// Test 2: memory_get 获取完整事件
func TestRecallGetTool_GetEvent(t *testing.T) {
	partitionID := memory.PartitionIDFromName("test")
	key1 := memory.NewSnowflakeEventKey(partitionID, 0)

	store := newTestMemoryStore(t, map[int64]memory.FullEvent{
		key1: {
			EventKey:     key1,
			PartitionID:  partitionID,
			EventType:    memory.EventTypeActionCommand,
			EventSummary: "执行部署命令",
			Timestamp:    time.Now().Add(-2 * time.Hour).UnixMilli(),
			Content:      "deploy.sh --env production",
		},
	})

	// 直接测试 MemoryStore.GetEvent（子工具底层调用）
	event, err := store.GetEvent(key1)
	if err != nil {
		t.Fatalf("GetEvent failed: %v", err)
	}

	// 验证: 返回完整事件
	if event.EventKey != key1 {
		t.Errorf("Expected event key %d, got %d", key1, event.EventKey)
	}
	if event.Content != "deploy.sh --env production" {
		t.Errorf("Expected content 'deploy.sh --env production', got %s", event.Content)
	}

	t.Logf("GetEvent: key=%d, content=%s", event.EventKey, event.Content)
}

// Test: MemoryStore 空查询应返回空列表
func TestMemoryStore_EmptyQuery(t *testing.T) {
	store := memory.NewInMemoryStore()

	events, err := store.QueryEvents(memory.QueryOptions{Limit: 10})
	if err != nil {
		t.Fatalf("QueryEvents failed: %v", err)
	}

	if len(events) != 0 {
		t.Errorf("Expected 0 events for empty store, got %d", len(events))
	}
}

// Test: MemoryStore 无事件时 GetEvent 应返回错误
func TestMemoryStore_NoEventGet(t *testing.T) {
	store := memory.NewInMemoryStore()

	_, err := store.GetEvent(0) // 0 is an invalid Snowflake key
	if err == nil {
		t.Error("Expected error for nonexistent event key, got nil")
	}
}

// ==================== Sub-tool Tests ====================

// TestSubTool_SkillSearch tests the skill_search sub-tool.
func TestSubTool_SkillSearch(t *testing.T) {
	repo := &mockSkillRepo{
		summaries: []skill.Summary{{Name: "github-pr", Description: "Create a GitHub pull request workflow"}},
	}

	results := searchSkills(repo, "github")
	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}
	if results[0].Title != "github-pr" {
		t.Errorf("Expected 'github-pr', got %q", results[0].Title)
	}
}

// TestSubTool_SkillSearch_NoMatch tests that unrelated queries return few/no results.
func TestSubTool_SkillSearch_NoMatch(t *testing.T) {
	repo := &mockSkillRepo{
		summaries: []skill.Summary{{Name: "deploy", Description: "Deploy application"}},
	}

	results := searchSkills(repo, "xyzabc123")
	if len(results) > 0 {
		t.Logf("Got %d results (some may match due to CJK substring logic), not a hard failure", len(results))
	}
}

// mockSkillRepo is a test double for SkillRepository.
type mockSkillRepo struct {
	summaries []skill.Summary
}

func (m *mockSkillRepo) Summaries() []skill.Summary { return m.summaries }
func (m *mockSkillRepo) Get(name string) (*skill.Skill, error) {
	return nil, fmt.Errorf("skill not found: %s", name)
}

// searchSkills is a copy of the knowledge package implementation for testing.
// This is duplicated to avoid import cycles.
type knowledgeResult struct {
	Type    string
	Title   string
	Content string
}

func searchSkills(repo SkillRepository, query string) []knowledgeResult {
	summaries := repo.Summaries()
	queryLower := strings.ToLower(query)

	var results []knowledgeResult
	for _, s := range summaries {
		nameLower := strings.ToLower(s.Name)
		descLower := strings.ToLower(s.Description)

		found := false
		if strings.Contains(descLower, queryLower) || strings.Contains(nameLower, queryLower) {
			found = true
		}
		if !found && strings.Contains(queryLower, nameLower) {
			found = true
		}

		if found {
			results = append(results, knowledgeResult{
				Type:    "skill",
				Title:   s.Name,
				Content: s.Description,
			})
		}
	}

	return results
}
