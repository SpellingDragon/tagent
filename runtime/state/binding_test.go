package state

import (
	"testing"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// toolFaceTool is the minimal native tool a face test registers: a declaration
// and nothing else.
type toolFaceTool struct {
	name string
}

// Declaration reports the tool declaration the face reads the name from.
func (t toolFaceTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{Name: t.name}
}

// TestToolFaceReportsOneFaceForListAndRequest checks the published tools lead in name order and later-registered tools follow.
//
// 契约: docs/wiki/agent/event-flow.md#e2e-turn-sequence
func TestToolFaceReportsOneFaceForListAndRequest(t *testing.T) {
	t.Run("later-registered tools trail the published ones", func(t *testing.T) {
		resources := NewToolResources("native-tools")
		resources.Register(ToolKindFunction, "contributed", toolFaceTool{name: "zeta"})
		binding := AgentBinding{
			Tools: []trpctool.Tool{toolFaceTool{name: "beta"}, toolFaceTool{name: "alpha"}},
			Set:   resources,
		}
		tools, order := ToolFace(binding)
		if len(tools) != 3 || len(order) != 3 {
			t.Fatalf("face = (%d tools, order %v), want three tools each named once", len(tools), order)
		}
		want := []string{"alpha", "beta", "zeta"}
		for at, name := range want {
			if order[at] != name {
				t.Fatalf("order = %v, want the published tools in name order then the later one (%v)", order, want)
			}
			if tools[name] == nil {
				t.Fatalf("tools carries no %q, want the list and the request to name the same set", name)
			}
		}
	})

	t.Run("a resource face without tools keeps the published face", func(t *testing.T) {
		binding := AgentBinding{Tools: []trpctool.Tool{toolFaceTool{name: "beta"}, toolFaceTool{name: "alpha"}}}
		tools, order := ToolFace(binding)
		if len(tools) != 2 || len(order) != 2 || order[0] != "alpha" || order[1] != "beta" {
			t.Fatalf("face = (%d tools, order %v), want the two published tools in name order", len(tools), order)
		}
	})
}

// TestToolFaceKeepsTheVisibleSubset checks a named selection keeps exactly its tools and a nil selection opens the full face.
func TestToolFaceKeepsTheVisibleSubset(t *testing.T) {
	resources := NewToolResources("native-tools")
	resources.Register(ToolKindFunction, "contributed", toolFaceTool{name: "zeta"})
	binding := AgentBinding{
		Tools: []trpctool.Tool{toolFaceTool{name: "beta"}, toolFaceTool{name: "alpha"}},
		Set:   resources,
	}
	resources.SetVisible(VisibleSelection{Tools: []string{"beta", "zeta"}})
	tools, order := ToolFace(binding)
	if len(order) != 2 || order[0] != "beta" || order[1] != "zeta" {
		t.Fatalf("order = %v, want the named subset in face order (beta, zeta)", order)
	}
	if _, held := tools["alpha"]; held {
		t.Fatalf("tools carries alpha beside a face narrowed without it: the request must not carry a hidden tool")
	}
	resources.SetVisible(VisibleSelection{})
	if _, order := ToolFace(binding); len(order) != 3 {
		t.Fatalf("order = %v, want the full face again after the selection opens", order)
	}
}

// TestToolFaceKeepsAnEmptySelectionNarrow checks a named-but-empty selection exposes nothing while a nil list is the full face.
func TestToolFaceKeepsAnEmptySelectionNarrow(t *testing.T) {
	resources := NewToolResources("native-tools")
	binding := AgentBinding{Tools: []trpctool.Tool{toolFaceTool{name: "alpha"}}, Set: resources}
	resources.SetVisible(VisibleSelection{Tools: []string{}})
	tools, order := ToolFace(binding)
	if len(tools) != 0 || len(order) != 0 {
		t.Fatalf("face = (%d tools, order %v), want nothing visible for an empty selection", len(tools), order)
	}
	if got := resources.Visible().Tools; got == nil {
		t.Fatalf("visible tools = nil, want the empty subset kept distinct from the full face")
	}
}

// TestToolFaceRefusesNoCallableTool checks every named tool answers from the same map the loop dispatches with.
func TestToolFaceRefusesNoCallableTool(t *testing.T) {
	resources := NewToolResources("native-tools", nil)
	resources.Register(ToolKindSkill, "roots", toolFaceTool{name: "skill_load"})
	binding := AgentBinding{Tools: []trpctool.Tool{toolFaceTool{name: "send"}}, Set: resources}
	tools, order := ToolFace(binding)
	if len(order) != 2 {
		t.Fatalf("order = %v, want the published tool and the registered one", order)
	}
	for _, name := range order {
		if tools[name] == nil {
			t.Fatalf("tools carries no %q while the order names it: the list would name a tool the request cannot hold", name)
		}
	}
}
