package shelldecomp

import tree_sitter "github.com/tree-sitter/go-tree-sitter"

func (walk *walker) handleFileReadRedirect(node *tree_sitter.Node, command Command, currentScope *scope) {
	input := false
	for index := range node.ChildCount() {
		child := node.Child(index)
		if child != nil && !child.IsNamed() && redirectOp(child.Kind()) == opRedirectIn {
			input = true
		}
	}
	if !input {
		return
	}
	destination := node.ChildByFieldName("destination")
	if destination == nil {
		return
	}
	kind := nodeKind(destination.Kind())
	if kind == kindProcessSubstitution || kind == kindCommandSubstitution || kind == kindFileDescriptor {
		return
	}
	token := resolveArgNode(destination, walk.source, currentScope)
	target := resolveReadTarget(command.Argv0, token, currentScope)
	target.Origin = ReadOriginRedirect
	if command.Node != nil {
		target.CommandStartByte = command.Node.StartByte
	}
	walk.result.reads = append(walk.result.reads, target)
}
