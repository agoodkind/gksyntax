package shelldecomp

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// walkAssignment records a persistent shell variable assignment and updates
// the current scope's literal assignment state.
func (walk *walker) walkAssignment(node *tree_sitter.Node, currentScope *scope) {
	assignment := extractAssignment(node, walk.source, currentScope.id)
	walk.result.assigns = append(walk.result.assigns, assignment)
	currentScope.applyAssignment(assignment)
}

// extractAssignment turns a variable_assignment node into a public Assignment.
func extractAssignment(node *tree_sitter.Node, source []byte, scopeID int) Assignment {
	assignment := Assignment{
		Name:       "",
		Value:      "",
		Resolvable: false,
		ScopeID:    scopeID,
		StartByte:  node.StartByte(),
		EndByte:    node.EndByte(),
		Text:       node.Utf8Text(source),
	}

	nameNode := node.ChildByFieldName("name")
	if nameNode != nil && nodeKind(nameNode.Kind()) == kindVariableName {
		name := nameNode.Utf8Text(source)
		if isShellName(name) {
			assignment.Name = name
		}
	}
	if assignment.Name == "" {
		return assignment
	}

	valueNode := node.ChildByFieldName("value")
	value := ""
	valueResolvable := true
	if valueNode != nil {
		value, valueResolvable = literalValue(valueNode, source)
	}
	if !valueResolvable || assignmentUsesAppend(nameNode, valueNode, source) {
		return assignment
	}

	assignment.Value = value
	assignment.Resolvable = true
	return assignment
}

// assignmentUsesAppend reports whether a variable_assignment uses += rather
// than =. Appends depend on previous runtime state, so they are not literal.
func assignmentUsesAppend(
	nameNode *tree_sitter.Node,
	valueNode *tree_sitter.Node,
	source []byte,
) bool {
	if nameNode == nil || valueNode == nil {
		return false
	}
	start := nameNode.EndByte()
	end := valueNode.StartByte()
	if start > end || end > uint(len(source)) {
		return false
	}
	operator := string(source[start:end])
	return strings.Contains(operator, "+=")
}
