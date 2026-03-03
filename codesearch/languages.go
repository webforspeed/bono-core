package codesearch

import (
	"path/filepath"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/bash"
	"github.com/smacker/go-tree-sitter/c"
	"github.com/smacker/go-tree-sitter/cpp"
	"github.com/smacker/go-tree-sitter/csharp"
	"github.com/smacker/go-tree-sitter/css"
	"github.com/smacker/go-tree-sitter/golang"
	"github.com/smacker/go-tree-sitter/java"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/python"
	"github.com/smacker/go-tree-sitter/ruby"
	"github.com/smacker/go-tree-sitter/rust"
	"github.com/smacker/go-tree-sitter/scala"
	"github.com/smacker/go-tree-sitter/swift"
	"github.com/smacker/go-tree-sitter/typescript/typescript"
	"github.com/smacker/go-tree-sitter/typescript/tsx"
)

// LanguageConfig maps a language to its tree-sitter grammar and AST node types to extract.
type LanguageConfig struct {
	Language  *sitter.Language
	NodeTypes []string // AST node type names to extract as chunks
}

// languageConfigs maps language names to their configs.
var languageConfigs = map[string]LanguageConfig{
	"go": {
		Language:  golang.GetLanguage(),
		NodeTypes: []string{"function_declaration", "method_declaration", "type_declaration"},
	},
	"python": {
		Language:  python.GetLanguage(),
		NodeTypes: []string{"function_definition", "class_definition"},
	},
	"javascript": {
		Language:  javascript.GetLanguage(),
		NodeTypes: []string{"function_declaration", "class_declaration", "arrow_function", "method_definition", "export_statement"},
	},
	"typescript": {
		Language:  typescript.GetLanguage(),
		NodeTypes: []string{"function_declaration", "class_declaration", "arrow_function", "method_definition", "interface_declaration", "type_alias_declaration", "export_statement"},
	},
	"tsx": {
		Language:  tsx.GetLanguage(),
		NodeTypes: []string{"function_declaration", "class_declaration", "arrow_function", "method_definition", "interface_declaration", "type_alias_declaration", "export_statement"},
	},
	"rust": {
		Language:  rust.GetLanguage(),
		NodeTypes: []string{"function_item", "impl_item", "struct_item", "enum_item", "trait_item"},
	},
	"java": {
		Language:  java.GetLanguage(),
		NodeTypes: []string{"method_declaration", "class_declaration", "interface_declaration", "enum_declaration"},
	},
	"c": {
		Language:  c.GetLanguage(),
		NodeTypes: []string{"function_definition", "struct_specifier", "enum_specifier"},
	},
	"cpp": {
		Language:  cpp.GetLanguage(),
		NodeTypes: []string{"function_definition", "class_specifier", "struct_specifier", "namespace_definition"},
	},
	"ruby": {
		Language:  ruby.GetLanguage(),
		NodeTypes: []string{"method", "class", "module"},
	},
	"swift": {
		Language:  swift.GetLanguage(),
		NodeTypes: []string{"function_declaration", "class_declaration", "struct_declaration", "protocol_declaration", "enum_declaration"},
	},
	"scala": {
		Language:  scala.GetLanguage(),
		NodeTypes: []string{"function_definition", "class_definition", "object_definition", "trait_definition"},
	},
	"csharp": {
		Language:  csharp.GetLanguage(),
		NodeTypes: []string{"method_declaration", "class_declaration", "interface_declaration", "struct_declaration"},
	},
	"bash": {
		Language:  bash.GetLanguage(),
		NodeTypes: []string{"function_definition"},
	},
	"css": {
		Language:  css.GetLanguage(),
		NodeTypes: []string{"rule_set", "media_statement"},
	},
}

// extensionToLanguage maps file extensions to language names.
var extensionToLanguage = map[string]string{
	".go":    "go",
	".py":    "python",
	".pyw":   "python",
	".js":    "javascript",
	".jsx":   "javascript",
	".mjs":   "javascript",
	".ts":    "typescript",
	".tsx":   "tsx",
	".rs":    "rust",
	".java":  "java",
	".c":     "c",
	".h":     "c",
	".cpp":   "cpp",
	".cc":    "cpp",
	".cxx":   "cpp",
	".hpp":   "cpp",
	".hxx":   "cpp",
	".rb":    "ruby",
	".swift": "swift",
	".scala": "scala",
	".sc":    "scala",
	".cs":    "csharp",
	".sh":    "bash",
	".bash":  "bash",
	".zsh":   "bash",
	".css":   "css",
}

// DetectLanguage returns the language name for a file path based on extension.
// Returns empty string for unsupported files.
func DetectLanguage(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	return extensionToLanguage[ext]
}

// GetLanguageConfig returns the tree-sitter config for a language, or nil if unsupported.
func GetLanguageConfig(lang string) *LanguageConfig {
	cfg, ok := languageConfigs[lang]
	if !ok {
		return nil
	}
	return &cfg
}

// IsTextFile returns true if the file extension suggests a text file worth indexing.
// This is broader than tree-sitter support — includes config files, docs, etc.
func IsTextFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	if extensionToLanguage[ext] != "" {
		return true
	}
	// Additional text file extensions worth indexing via fallback chunker
	textExts := map[string]bool{
		".md": true, ".txt": true, ".rst": true,
		".yaml": true, ".yml": true, ".toml": true, ".json": true, ".xml": true,
		".html": true, ".htm": true,
		".sql": true, ".graphql": true, ".gql": true,
		".proto": true, ".thrift": true,
		".dockerfile": true, ".makefile": true,
		".env": true, ".ini": true, ".cfg": true, ".conf": true,
		".tf": true, ".hcl": true,
		".gradle": true, ".cmake": true,
		".r": true, ".R": true, ".jl": true,
		".lua": true, ".pl": true, ".pm": true, ".php": true,
		".ex": true, ".exs": true, ".erl": true, ".hrl": true,
		".hs": true, ".ml": true, ".mli": true,
		".kt": true, ".kts": true, ".groovy": true,
		".dart": true, ".v": true, ".zig": true, ".nim": true,
	}
	if textExts[ext] {
		return true
	}
	// Files with no extension might be text (Makefile, Dockerfile, etc.)
	base := strings.ToLower(filepath.Base(path))
	nameFiles := map[string]bool{
		"makefile": true, "dockerfile": true, "jenkinsfile": true,
		"gemfile": true, "rakefile": true, "procfile": true,
		"vagrantfile": true, "cmakelists.txt": true,
	}
	return nameFiles[base]
}
