// Package recorder — snapshot-based ref resolution for stable recording.
package recorder

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// ElementFingerprint is a stable identifier extracted from an AX node.
// Used to match the same logical element across browser sessions.
type ElementFingerprint struct {
	Role  string `json:"role"`
	Name  string `json:"name"`
	Value string `json:"value,omitempty"`
}

// axNode mirrors a single node in the CDP Accessibility.getFullAXTree response.
// Only the fields we need for fingerprint matching.
type axNode struct {
	NodeID           string   `json:"nodeId"`
	BackendDOMNodeID int      `json:"backendDOMNodeId"`
	Ignored          bool     `json:"ignored"`
	Role             axValue  `json:"role"`
	Name             axValue  `json:"name"`
	Value            axValue  `json:"value"`
	ChildIDs         []string `json:"childIds"`
}

type axValue struct {
	Value any    `json:"value"`
	Type  string `json:"type"`
}

type axTree struct {
	Nodes []axNode `json:"nodes"`
}

// IsRefTool reports whether tool is a _ref variant (uses snapshot backendDOMNodeId).
func IsRefTool(name string) bool {
	switch name {
	case "browser_click_ref",
		"browser_type_ref", "browser_fill_ref",
		"browser_hover_ref", "browser_focus_ref",
		"browser_select_option_ref", "browser_check_ref",
		"browser_uncheck_ref":
		return true
	}
	return false
}

// FindNodeByRef searches the snapshot JSON for the node whose backendDOMNodeId
// matches the given ref string.
func FindNodeByRef(snapshot json.RawMessage, ref string) (*axNode, error) {
	if len(snapshot) == 0 {
		return nil, fmt.Errorf("snapshot is empty")
	}
	var tree axTree
	if err := json.Unmarshal(snapshot, &tree); err != nil {
		return nil, fmt.Errorf("snapshot parse: %w", err)
	}
	refInt, err := strconv.Atoi(ref)
	if err != nil {
		return nil, fmt.Errorf("invalid ref %q: %w", ref, err)
	}
	for i := range tree.Nodes {
		if tree.Nodes[i].BackendDOMNodeID == refInt {
			return &tree.Nodes[i], nil
		}
	}
	return nil, fmt.Errorf("ref %s not found in snapshot (%d nodes)", ref, len(tree.Nodes))
}

// FingerprintFromNode extracts a stable fingerprint from an AX node.
func FingerprintFromNode(node *axNode) ElementFingerprint {
	return ElementFingerprint{
		Role:  axStr(node.Role),
		Name:  axStr(node.Name),
		Value: axStr(node.Value),
	}
}

// FindNodeByFingerprint searches the snapshot for a non-ignored node matching fp.
// Tries exact match (role+name+value) first, then falls back to role+name only.
func FindNodeByFingerprint(snapshot json.RawMessage, fp ElementFingerprint) (*axNode, error) {
	var tree axTree
	if err := json.Unmarshal(snapshot, &tree); err != nil {
		return nil, fmt.Errorf("snapshot parse: %w", err)
	}

	// Pass 1: exact match (role + name + value).
	for i := range tree.Nodes {
		n := &tree.Nodes[i]
		if n.Ignored {
			continue
		}
		if axStr(n.Role) == fp.Role && axStr(n.Name) == fp.Name {
			if fp.Value == "" || axStr(n.Value) == fp.Value {
				return n, nil
			}
		}
	}

	// Pass 2: role + name only (value may have changed).
	for i := range tree.Nodes {
		n := &tree.Nodes[i]
		if n.Ignored {
			continue
		}
		if axStr(n.Role) == fp.Role && axStr(n.Name) == fp.Name {
			return n, nil
		}
	}

	return nil, fmt.Errorf("element not found: role=%q name=%q", fp.Role, fp.Name)
}

func axStr(v axValue) string {
	if s, ok := v.Value.(string); ok {
		return s
	}
	return ""
}
