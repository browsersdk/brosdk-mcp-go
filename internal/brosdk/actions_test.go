//go:build windows || darwin

package brosdk

import (
	"encoding/json"
	"testing"
)

func makeTestSnapshot(nodes []axNode) json.RawMessage {
	b, _ := json.Marshal(axTree{Nodes: nodes})
	return b
}

func TestFilterRefs_ByRole(t *testing.T) {
	snap := makeTestSnapshot([]axNode{
		{Ignored: false, BackendDOMNodeID: 1, Role: axValue{Value: "button"}, Name: axValue{Value: "OK"}},
		{Ignored: false, BackendDOMNodeID: 2, Role: axValue{Value: "textbox"}, Name: axValue{Value: "Email"}},
		{Ignored: false, BackendDOMNodeID: 3, Role: axValue{Value: "button"}, Name: axValue{Value: "Cancel"}},
	})

	refs, err := filterRefs(snap, "button", "", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 2 {
		t.Fatalf("expected 2, got %d", len(refs))
	}
}

func TestFilterRefs_ByRoleAndName(t *testing.T) {
	snap := makeTestSnapshot([]axNode{
		{Ignored: false, BackendDOMNodeID: 1, Role: axValue{Value: "button"}, Name: axValue{Value: "OK"}},
		{Ignored: false, BackendDOMNodeID: 2, Role: axValue{Value: "button"}, Name: axValue{Value: "Cancel"}},
	})

	refs, err := filterRefs(snap, "button", "OK", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0].Ref != 1 {
		t.Fatalf("expected single ref=1, got %+v", refs)
	}
}

func TestFilterRefs_SkipsIgnored(t *testing.T) {
	snap := makeTestSnapshot([]axNode{
		{Ignored: true, BackendDOMNodeID: 1, Role: axValue{Value: "button"}, Name: axValue{Value: "Hidden"}},
		{Ignored: false, BackendDOMNodeID: 2, Role: axValue{Value: "button"}, Name: axValue{Value: "Visible"}},
	})

	refs, err := filterRefs(snap, "button", "", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0].Ref != 2 {
		t.Fatal("should skip ignored node")
	}
}

func TestFilterRefs_WithLimit(t *testing.T) {
	snap := makeTestSnapshot([]axNode{
		{Ignored: false, BackendDOMNodeID: 1, Role: axValue{Value: "button"}, Name: axValue{Value: "A"}},
		{Ignored: false, BackendDOMNodeID: 2, Role: axValue{Value: "button"}, Name: axValue{Value: "B"}},
		{Ignored: false, BackendDOMNodeID: 3, Role: axValue{Value: "button"}, Name: axValue{Value: "C"}},
	})

	refs, err := filterRefs(snap, "button", "", "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 2 {
		t.Fatalf("limit should cap at 2, got %d", len(refs))
	}
}

func TestFilterRefs_NoFiltersReturnsAllNonIgnored(t *testing.T) {
	snap := makeTestSnapshot([]axNode{
		{Ignored: true, BackendDOMNodeID: 1, Role: axValue{Value: "statictext"}},
		{Ignored: false, BackendDOMNodeID: 2, Role: axValue{Value: "button"}},
		{Ignored: false, BackendDOMNodeID: 3, Role: axValue{Value: "link"}},
	})

	refs, err := filterRefs(snap, "", "", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 2 {
		t.Fatalf("expected 2 non-ignored, got %d", len(refs))
	}
}

func TestFindNodeByFingerprintJSON_Found(t *testing.T) {
	snap := makeTestSnapshot([]axNode{
		{Ignored: false, BackendDOMNodeID: 42, Role: axValue{Value: "button"}, Name: axValue{Value: "Login"}},
	})

	n, err := findNodeByFingerprintJSON(snap, "button", "Login", "")
	if err != nil {
		t.Fatal(err)
	}
	if n.BackendDOMNodeID != 42 {
		t.Fatalf("expected 42, got %d", n.BackendDOMNodeID)
	}
}

func TestFindNodeByFingerprintJSON_NotFound(t *testing.T) {
	snap := makeTestSnapshot([]axNode{
		{Ignored: false, BackendDOMNodeID: 42, Role: axValue{Value: "button"}, Name: axValue{Value: "Login"}},
	})

	_, err := findNodeByFingerprintJSON(snap, "button", "Register", "")
	if err == nil {
		t.Fatal("expected error for missing element")
	}
}

func TestFindNodeByFingerprintJSON_SkipsIgnored(t *testing.T) {
	snap := makeTestSnapshot([]axNode{
		{Ignored: true, BackendDOMNodeID: 1, Role: axValue{Value: "button"}, Name: axValue{Value: "Submit"}},
		{Ignored: false, BackendDOMNodeID: 2, Role: axValue{Value: "button"}, Name: axValue{Value: "Submit"}},
	})

	n, err := findNodeByFingerprintJSON(snap, "button", "Submit", "")
	if err != nil {
		t.Fatal(err)
	}
	if n.BackendDOMNodeID != 2 {
		t.Fatal("should return non-ignored node")
	}
}

func TestFilterInteractiveNodes(t *testing.T) {
	snap := makeTestSnapshot([]axNode{
		{Ignored: false, BackendDOMNodeID: 1, Role: axValue{Value: "button"}, Name: axValue{Value: "OK"}},
		{Ignored: false, BackendDOMNodeID: 2, Role: axValue{Value: "statictext"}, Name: axValue{Value: "Hello world"}},
		{Ignored: false, BackendDOMNodeID: 3, Role: axValue{Value: "textbox"}, Name: axValue{Value: "Email"}},
		{Ignored: false, BackendDOMNodeID: 4, Role: axValue{Value: "generic"}, Name: axValue{Value: "Container"}},
		{Ignored: true, BackendDOMNodeID: 5, Role: axValue{Value: "link"}, Name: axValue{Value: "Hidden"}},
	})

	out, err := filterInteractiveNodes(snap)
	if err != nil {
		t.Fatal(err)
	}

	var tree axTree
	json.Unmarshal(out, &tree)

	// Should keep: button(1), textbox(3). Skip: statictext(2), generic(4), hidden-link(5)
	if len(tree.Nodes) != 2 {
		t.Fatalf("expected 2 interactive nodes, got %d", len(tree.Nodes))
	}

	ids := map[int]bool{}
	for _, n := range tree.Nodes {
		ids[n.BackendDOMNodeID] = true
	}
	if !ids[1] || !ids[3] {
		t.Fatalf("expected nodes 1 and 3 in output, got %v", ids)
	}
}

func TestAxStr(t *testing.T) {
	if s := axStr(axValue{Value: "hello", Type: "string"}); s != "hello" {
		t.Fatalf("expected 'hello', got %q", s)
	}
	if s := axStr(axValue{Value: 42, Type: "integer"}); s != "" {
		t.Fatalf("expected empty for non-string, got %q", s)
	}
	if s := axStr(axValue{Value: nil}); s != "" {
		t.Fatalf("expected empty for nil, got %q", s)
	}
}
