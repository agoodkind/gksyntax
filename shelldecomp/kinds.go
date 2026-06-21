package shelldecomp

// nodeKind is a named type over tree-sitter node kind strings so kind switches
// use a declared enum rather than bare string literals. The constants name the
// bash grammar nodes shelldecomp walks.
type nodeKind string

// Bash grammar node kinds shelldecomp recognizes while walking a parse tree.
const (
	kindProgram             nodeKind = "program"
	kindList                nodeKind = "list"
	kindCompoundStatement   nodeKind = "compound_statement"
	kindPipeline            nodeKind = "pipeline"
	kindSubshell            nodeKind = "subshell"
	kindRedirectedStatement nodeKind = "redirected_statement"
	kindCommand             nodeKind = "command"
	kindVariableAssignment  nodeKind = "variable_assignment"
	kindVariableAssignments nodeKind = "variable_assignments"
	kindVariableName        nodeKind = "variable_name"
	kindFileRedirect        nodeKind = "file_redirect"
	kindHeredocRedirect     nodeKind = "heredoc_redirect"
	kindHeredocBody         nodeKind = "heredoc_body"
	kindWord                nodeKind = "word"
	kindRawString           nodeKind = "raw_string"
	kindString              nodeKind = "string"
	kindStringContent       nodeKind = "string_content"
	kindConcatenation       nodeKind = "concatenation"
	kindSimpleExpansion     nodeKind = "simple_expansion"
	kindExpansion           nodeKind = "expansion"
	kindCommandSubstitution nodeKind = "command_substitution"
	kindProcessSubstitution nodeKind = "process_substitution"
	kindNumber              nodeKind = "number"
	kindFileDescriptor      nodeKind = "file_descriptor"
	kindDoubleQuote         nodeKind = "\""
)

// redirectOp is a named type over the operator tokens of a file_redirect so the
// output-versus-input test switches on declared constants.
type redirectOp string

// File redirect operator tokens. The output operators write a file; the input
// operators read one.
const (
	opRedirectOut       redirectOp = ">"
	opRedirectAppend    redirectOp = ">>"
	opRedirectAll       redirectOp = "&>"
	opRedirectAllAmp    redirectOp = ">&"
	opRedirectAllAppend redirectOp = "&>>"
	opRedirectIn        redirectOp = "<"
	opRedirectHeredoc   redirectOp = "<<"
	opRedirectHereStr   redirectOp = "<<<"
	opRedirectHeredocND redirectOp = "<<-"
)
