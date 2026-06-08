// Package chunk splits code files into searchable chunks using a tree-sitter
// AST when a grammar is available and a recursive-separator fallback when one
// is not. It depends on the treesitter package for grammar lookup and the file
// extension to language map.
package chunk

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"unicode/utf8"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	"goodkind.io/gksyntax/treesitter"
)

// Chunk is one code chunk emitted by a splitter.
type Chunk struct {
	Content   string
	StartLine int
	EndLine   int
	Language  string
	FilePath  string
}

// Result captures the emitted chunks and which strategy produced them.
type Result struct {
	Chunks   []Chunk
	Strategy string
}

// Dispatcher routes files to AST or fallback splitting.
type Dispatcher struct {
	astChunkSize      int
	astChunkOverlap   int
	fallbackChunkSize int
	fallbackOverlap   int
}

const splitterTypeLangchain = "langchain"

// NewDispatcher constructs a splitter dispatcher using the current defaults.
func NewDispatcher() *Dispatcher {
	return &Dispatcher{
		astChunkSize:      2500,
		astChunkOverlap:   300,
		fallbackChunkSize: 1000,
		fallbackOverlap:   200,
	}
}

// SplitFile splits one file into chunks using AST or fallback behavior.
func (dispatcher *Dispatcher) SplitFile(ctx context.Context, path string, content []byte) (Result, error) {
	return dispatcher.SplitFileWithType(ctx, path, content, "")
}

// SplitFileWithType splits one file using the requested splitter type.
func (dispatcher *Dispatcher) SplitFileWithType(ctx context.Context, path string, content []byte, splitterType string) (Result, error) {
	language := treesitter.LanguageForPath(path)
	if strings.EqualFold(strings.TrimSpace(splitterType), splitterTypeLangchain) {
		return Result{
			Chunks:   langchainSplit(string(content), language, path, dispatcher.fallbackChunkSize, dispatcher.fallbackOverlap),
			Strategy: splitterTypeLangchain,
		}, nil
	}

	astChunks, err := dispatcher.tryAST(ctx, content, language, path)
	if err == nil {
		return Result{Chunks: astChunks, Strategy: "ast"}, nil
	}

	slog.WarnContext(ctx, "fall back to recursive separator splitter", "path", path, "language", language, "err", err)
	return Result{
		Chunks:   langchainSplit(string(content), language, path, dispatcher.fallbackChunkSize, dispatcher.fallbackOverlap),
		Strategy: "fallback",
	}, nil
}

func (dispatcher *Dispatcher) tryAST(ctx context.Context, content []byte, language string, path string) ([]Chunk, error) {
	grammar, supported := treesitter.GrammarForLanguage(language)
	if !supported {
		slog.WarnContext(ctx, "language is unsupported by AST splitter", "path", path, "language", language)
		return nil, fmt.Errorf("language %s not supported by AST splitter", language)
	}

	parser := tree_sitter.NewParser()
	defer parser.Close()

	if err := parser.SetLanguage(grammar); err != nil {
		slog.ErrorContext(ctx, "set parser language failed", "path", path, "language", language, "err", err)
		return nil, fmt.Errorf("set parser language for %s: %w", language, err)
	}

	tree := parser.Parse(content, nil)
	if tree == nil {
		slog.ErrorContext(ctx, "parse returned nil tree", "path", path, "language", language, "err", errors.New("nil tree"))
		return nil, fmt.Errorf("parse returned nil tree for %s", path)
	}
	defer tree.Close()

	rootNode := tree.RootNode()
	if rootNode == nil {
		slog.ErrorContext(ctx, "parse produced no root node", "path", path, "language", language, "err", errors.New("nil root node"))
		return nil, fmt.Errorf("parse produced no root node for %s", path)
	}

	chunks := dispatcher.chunkNode(rootNode, content, language, path, dispatcher.astChunkSize)
	if len(chunks) == 0 && strings.TrimSpace(string(content)) != "" {
		chunks = langchainSplit(string(content), language, path, dispatcher.astChunkSize, 0)
	}
	return addOverlap(chunks, dispatcher.astChunkOverlap), nil
}

// chunkNode walks the parse tree and returns size-balanced chunks following the
// cAST method: a node whose non-whitespace size fits the budget becomes one
// chunk, a larger node is split into its children whose chunks are greedily
// merged back up to the budget, and a larger node with no children is split on
// textual seams. The budget counts non-whitespace bytes so indentation and
// blank lines do not fragment otherwise-coherent chunks.
func (dispatcher *Dispatcher) chunkNode(node *tree_sitter.Node, content []byte, language string, path string, budget int) []Chunk {
	if node == nil {
		return nil
	}
	if nonWhitespaceByteCount(content, node.StartByte(), node.EndByte()) <= budget {
		if chunk, ok := chunkFromBytes(content, node.StartByte(), node.EndByte(), node.StartPosition(), node.EndPosition(), language, path); ok {
			return []Chunk{chunk}
		}
		return nil
	}
	if node.ChildCount() == 0 {
		return dispatcher.splitOversizeSpan(node, content, language, path)
	}
	return dispatcher.mergeChildChunks(node, content, language, path, budget)
}

// mergeChildChunks greedily groups consecutive children of an oversize node into
// chunks up to the budget. A child that alone exceeds the budget is chunked on
// its own by recursing into chunkNode. A merged group spans from its first
// child's start byte to its last child's end byte, so comments and punctuation
// between children stay inside the chunk.
func (dispatcher *Dispatcher) mergeChildChunks(node *tree_sitter.Node, content []byte, language string, path string, budget int) []Chunk {
	chunks := make([]Chunk, 0)
	var groupStartByte, groupEndByte uint
	var groupStartPos, groupEndPos tree_sitter.Point
	groupSize := 0
	haveGroup := false

	flushGroup := func() {
		if !haveGroup {
			return
		}
		if chunk, ok := chunkFromBytes(content, groupStartByte, groupEndByte, groupStartPos, groupEndPos, language, path); ok {
			chunks = append(chunks, chunk)
		}
		haveGroup = false
		groupSize = 0
	}

	for index := range node.ChildCount() {
		child := node.Child(index)
		if child == nil {
			continue
		}
		childSize := nonWhitespaceByteCount(content, child.StartByte(), child.EndByte())
		if childSize > budget {
			flushGroup()
			chunks = append(chunks, dispatcher.chunkNode(child, content, language, path, budget)...)
			continue
		}
		if haveGroup && groupSize+childSize > budget {
			flushGroup()
		}
		if !haveGroup {
			groupStartByte = child.StartByte()
			groupStartPos = child.StartPosition()
			haveGroup = true
		}
		groupEndByte = child.EndByte()
		groupEndPos = child.EndPosition()
		groupSize += childSize
	}
	flushGroup()
	return chunks
}

// splitOversizeSpan splits a single node that exceeds the budget and has no
// children to recurse into. It cuts on language-aware seams through the
// recursive separator splitter, whose byte-based hard split keeps any single
// piece within the per-chunk byte cap the vector store enforces. Line numbers
// are offset back to the node's position in the file.
func (dispatcher *Dispatcher) splitOversizeSpan(node *tree_sitter.Node, content []byte, language string, path string) []Chunk {
	startByte := node.StartByte()
	endByte := node.EndByte()
	if startByte >= endByte || int(endByte) > len(content) {
		return nil
	}
	baseLine := safeInt(node.StartPosition().Row)
	pieces := langchainSplit(string(content[startByte:endByte]), language, path, dispatcher.astChunkSize, 0)
	for index := range pieces {
		pieces[index].StartLine += baseLine
		pieces[index].EndLine += baseLine
	}
	return pieces
}

// chunkFromBytes builds a chunk from a byte span, trimming surrounding
// whitespace. It returns false when the span is empty or whitespace-only.
func chunkFromBytes(content []byte, startByte uint, endByte uint, startPos tree_sitter.Point, endPos tree_sitter.Point, language string, path string) (Chunk, bool) {
	var zero Chunk
	if startByte >= endByte || int(endByte) > len(content) {
		return zero, false
	}
	text := strings.TrimSpace(string(content[startByte:endByte]))
	if text == "" {
		return zero, false
	}
	return Chunk{
		Content:   text,
		StartLine: safeInt(startPos.Row) + 1,
		EndLine:   safeInt(endPos.Row) + 1,
		Language:  language,
		FilePath:  path,
	}, true
}

// nonWhitespaceByteCount counts the non-whitespace bytes in content[start:end].
// The cAST budget uses this measure so indentation and blank lines do not
// inflate a node's size and fragment otherwise-coherent chunks.
func nonWhitespaceByteCount(content []byte, start uint, end uint) int {
	if start >= end || int(end) > len(content) {
		return 0
	}
	count := 0
	for index := start; index < end; index++ {
		switch content[index] {
		case ' ', '\t', '\n', '\r', '\f', '\v':
		default:
			count++
		}
	}
	return count
}

func addOverlap(chunks []Chunk, overlap int) []Chunk {
	if len(chunks) <= 1 || overlap <= 0 {
		return chunks
	}
	result := make([]Chunk, 0, len(chunks))
	result = append(result, chunks[0])
	for index := 1; index < len(chunks); index++ {
		chunk := chunks[index]
		previousChunk := chunks[index-1]
		overlapText := previousChunk.Content
		if len(overlapText) > overlap {
			cut := alignToRuneStart(overlapText, len(overlapText)-overlap)
			overlapText = overlapText[cut:]
		}
		chunk.Content = overlapText + "\n" + chunk.Content
		chunk.StartLine = max(1, chunk.StartLine-lineCount(overlapText))
		result = append(result, chunk)
	}
	return result
}

// alignToRuneStart returns the smallest index >= offset where s[i] starts a
// valid UTF-8 codepoint. Byte-offset slicing into UTF-8 strings without this
// alignment can yield chunk content that begins with a continuation byte,
// which Milvus rejects at the gRPC marshal boundary for VarChar fields.
func alignToRuneStart(s string, offset int) int {
	if offset <= 0 {
		return 0
	}
	if offset >= len(s) {
		return len(s)
	}
	for offset < len(s) && !utf8.RuneStart(s[offset]) {
		offset++
	}
	return offset
}

// alignDownToRuneStart returns the largest index <= offset where s[i] is the
// start of a codepoint (or i == 0). Used to trim a chunk's tail back to the
// last whole codepoint before a byte-offset boundary.
func alignDownToRuneStart(s string, offset int) int {
	if offset <= 0 {
		return 0
	}
	if offset >= len(s) {
		return len(s)
	}
	for offset > 0 && !utf8.RuneStart(s[offset]) {
		offset--
	}
	return offset
}

func lineCount(text string) int {
	if text == "" {
		return 0
	}
	return len(strings.Split(text, "\n"))
}

func safeInt(value uint) int {
	if value > math.MaxInt {
		return math.MaxInt
	}
	return int(value)
}
