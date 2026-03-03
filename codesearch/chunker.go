package codesearch

import (
	"context"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
)

const (
	maxChunkLines      = 100 // hard limit per chunk
	minChunkLines      = 3   // chunks smaller than this are merged or discarded
	fallbackWindow     = 40  // lines per window for fallback chunking
	fallbackOverlap    = 10  // overlap between windows
	largeNodeThreshold = 50  // split AST nodes larger than this
)

// ChunkFile parses a source file and returns semantic chunks.
// Uses tree-sitter for supported languages, falls back to line-based chunking.
func ChunkFile(path string, content []byte, lang string) []Chunk {
	if len(content) == 0 {
		return nil
	}

	cfg := GetLanguageConfig(lang)
	if cfg != nil {
		chunks := chunkWithTreeSitter(path, content, lang, cfg)
		if len(chunks) > 0 {
			return chunks
		}
	}

	// Fallback: line-based chunking
	return chunkByLines(path, content, lang)
}

// chunkWithTreeSitter uses AST parsing to extract semantic code chunks.
func chunkWithTreeSitter(path string, content []byte, lang string, cfg *LanguageConfig) (chunks []Chunk) {
	defer func() {
		if recover() != nil {
			chunks = nil
		}
	}()

	if cfg == nil || cfg.Language == nil {
		return nil
	}

	parser := sitter.NewParser()
	if parser == nil {
		return nil
	}
	defer parser.Close()

	parser.SetLanguage(cfg.Language)

	tree, err := parser.ParseCtx(context.Background(), nil, content)
	if err != nil {
		return nil
	}
	defer tree.Close()

	root := tree.RootNode()
	lines := strings.Split(string(content), "\n")

	// Build a set of target node types for fast lookup
	nodeTypes := make(map[string]bool, len(cfg.NodeTypes))
	for _, nt := range cfg.NodeTypes {
		nodeTypes[nt] = true
	}

	covered := make(map[int]bool) // line numbers covered by extracted nodes

	// Walk the AST and extract target nodes
	var walk func(node *sitter.Node)
	walk = func(node *sitter.Node) {
		if node == nil {
			return
		}

		if nodeTypes[node.Type()] {
			startLine := int(node.StartPoint().Row) + 1
			endLine := int(node.EndPoint().Row) + 1
			nodeLines := endLine - startLine + 1

			// Extract symbol name
			symbolName := extractSymbolName(node, content)
			chunkType := nodeTypeToChunkType(node.Type())

			if nodeLines > largeNodeThreshold {
				// Split large nodes into sub-chunks
				subChunks := splitLargeNode(path, lines, startLine, endLine, lang, symbolName, chunkType)
				chunks = append(chunks, subChunks...)
			} else if nodeLines >= minChunkLines {
				text := extractLines(lines, startLine, endLine)
				chunks = append(chunks, Chunk{
					Content:    text,
					FilePath:   path,
					StartLine:  startLine,
					EndLine:    endLine,
					ChunkType:  chunkType,
					SymbolName: symbolName,
					Language:   lang,
				})
			}

			// Mark lines as covered
			for l := startLine; l <= endLine; l++ {
				covered[l] = true
			}
			return // don't recurse into already-extracted nodes
		}

		// Recurse into children
		for i := 0; i < int(node.ChildCount()); i++ {
			walk(node.Child(i))
		}
	}

	walk(root)

	// Collect gap regions (uncovered code between extracted nodes)
	chunks = append(chunks, collectGaps(path, lines, covered, lang)...)

	return chunks
}

// extractSymbolName tries to find the identifier/name of an AST node.
func extractSymbolName(node *sitter.Node, content []byte) string {
	// Look for a direct "name" or "identifier" child
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case "identifier", "name", "property_identifier", "type_identifier":
			return child.Content(content)
		}
	}
	// For some languages, the name is nested deeper
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == "declarator" || child.Type() == "type_spec" {
			for j := 0; j < int(child.ChildCount()); j++ {
				gc := child.Child(j)
				if gc.Type() == "identifier" || gc.Type() == "type_identifier" {
					return gc.Content(content)
				}
			}
		}
	}
	return ""
}

// nodeTypeToChunkType maps tree-sitter node types to our chunk type taxonomy.
func nodeTypeToChunkType(nodeType string) string {
	switch nodeType {
	case "function_declaration", "function_definition", "function_item", "arrow_function":
		return "function"
	case "method_declaration", "method_definition", "method":
		return "method"
	case "class_declaration", "class_definition", "class_specifier", "class":
		return "class"
	case "type_declaration", "type_alias_declaration", "interface_declaration",
		"struct_specifier", "struct_item", "struct_declaration",
		"enum_specifier", "enum_item", "enum_declaration",
		"trait_item", "protocol_declaration",
		"object_definition", "trait_definition":
		return "struct"
	case "impl_item":
		return "method"
	case "namespace_definition", "module":
		return "code"
	case "export_statement":
		return "function"
	case "comment", "block_comment", "line_comment":
		return "comment"
	default:
		return "code"
	}
}

// splitLargeNode breaks a large AST node into manageable chunks.
func splitLargeNode(path string, lines []string, startLine, endLine int, lang, symbolName, chunkType string) []Chunk {
	var chunks []Chunk
	windowSize := maxChunkLines
	overlap := 10

	for pos := startLine; pos <= endLine; {
		end := pos + windowSize - 1
		if end > endLine {
			end = endLine
		}
		// Don't create tiny trailing chunks
		if end-pos+1 < minChunkLines && len(chunks) > 0 {
			break
		}

		text := extractLines(lines, pos, end)
		name := symbolName
		if len(chunks) > 0 {
			name = symbolName + " (cont.)"
		}

		chunks = append(chunks, Chunk{
			Content:    text,
			FilePath:   path,
			StartLine:  pos,
			EndLine:    end,
			ChunkType:  chunkType,
			SymbolName: name,
			Language:   lang,
		})

		pos = end - overlap + 1
		if pos <= chunks[len(chunks)-1].StartLine {
			pos = end + 1
		}
	}
	return chunks
}

// collectGaps finds uncovered regions between extracted AST nodes.
func collectGaps(path string, lines []string, covered map[int]bool, lang string) []Chunk {
	var chunks []Chunk
	totalLines := len(lines)

	gapStart := -1
	for i := 1; i <= totalLines; i++ {
		if covered[i] {
			if gapStart > 0 && i-gapStart >= minChunkLines+2 {
				text := extractLines(lines, gapStart, i-1)
				if strings.TrimSpace(text) != "" {
					chunks = append(chunks, Chunk{
						Content:   text,
						FilePath:  path,
						StartLine: gapStart,
						EndLine:   i - 1,
						ChunkType: "code",
						Language:  lang,
					})
				}
			}
			gapStart = -1
		} else if gapStart < 0 {
			gapStart = i
		}
	}

	// Trailing gap
	if gapStart > 0 && totalLines-gapStart+1 >= minChunkLines {
		text := extractLines(lines, gapStart, totalLines)
		if strings.TrimSpace(text) != "" {
			chunks = append(chunks, Chunk{
				Content:   text,
				FilePath:  path,
				StartLine: gapStart,
				EndLine:   totalLines,
				ChunkType: "code",
				Language:  lang,
			})
		}
	}

	return chunks
}

// chunkByLines splits a file into overlapping line windows (fallback for unsupported languages).
func chunkByLines(path string, content []byte, lang string) []Chunk {
	lines := strings.Split(string(content), "\n")
	totalLines := len(lines)
	if totalLines == 0 {
		return nil
	}

	// Small files: single chunk
	if totalLines <= fallbackWindow {
		text := string(content)
		if strings.TrimSpace(text) == "" {
			return nil
		}
		return []Chunk{{
			Content:   text,
			FilePath:  path,
			StartLine: 1,
			EndLine:   totalLines,
			ChunkType: "code",
			Language:  lang,
		}}
	}

	var chunks []Chunk
	for pos := 0; pos < totalLines; pos += fallbackWindow - fallbackOverlap {
		end := pos + fallbackWindow
		if end > totalLines {
			end = totalLines
		}
		// Don't create tiny trailing chunks
		if end-pos < minChunkLines && len(chunks) > 0 {
			break
		}

		text := extractLines(lines, pos+1, end)
		if strings.TrimSpace(text) == "" {
			continue
		}

		chunks = append(chunks, Chunk{
			Content:   text,
			FilePath:  path,
			StartLine: pos + 1,
			EndLine:   end,
			ChunkType: "code",
			Language:  lang,
		})
	}

	return chunks
}

// extractLines returns lines startLine..endLine (1-indexed, inclusive) joined by newlines.
func extractLines(lines []string, startLine, endLine int) string {
	if startLine < 1 {
		startLine = 1
	}
	if endLine > len(lines) {
		endLine = len(lines)
	}
	if startLine > endLine {
		return ""
	}
	return strings.Join(lines[startLine-1:endLine], "\n")
}
