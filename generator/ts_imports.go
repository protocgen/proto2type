package generator

import (
	"path"
	"sort"
	"strings"
)

// tsImportEntry represents a group of names to import from a single module.
type tsImportEntry struct {
	path  string   // relative ESM import path (e.g. "./other.type.js")
	names []string // sorted list of identifiers to import
}

// collectTSImports scans the IR for cross-file type references and returns
// grouped, deduplicated import entries sorted by path.
//
// TODO(H6): Cross-File Broken Imports limitation.
// Currently this blindly generates imports for ALL referenced types regardless of
// whether those files are being generated. In the future, this should take a set
// of GeneratedFiles to skip imports for files not in the generate list.
// Note: This limitation is documented here per H6 findings.
func collectTSImports(ir *DomainFile, opts *Options) []tsImportEntry {
	// Map from proto source path → set of type names needed.
	needed := make(map[string]map[string]bool)

	addRef := func(sourcePath, typeName string) {
		if sourcePath == "" || typeName == "" {
			return
		}
		if needed[sourcePath] == nil {
			needed[sourcePath] = make(map[string]bool)
		}
		// In types-only mode, skip Schema imports (no Zod runtime).
		if !opts.TSTypesOnly {
			needed[sourcePath][typeName+"Schema"] = true
		}
		needed[sourcePath][typeName] = true
	}

	for _, m := range ir.Messages {
		for _, f := range m.Fields {
			if f.MessageSourcePath != "" {
				addRef(f.MessageSourcePath, f.MessageTypeName)
			}
			if f.EnumSourcePath != "" {
				addRef(f.EnumSourcePath, f.EnumTypeName)
			}
			// Check map value types for cross-file references.
			if f.IsMap && f.MapValue != nil && f.MapValue.SourcePath != "" {
				addRef(f.MapValue.SourcePath, f.MapValue.MessageTypeName)
				addRef(f.MapValue.SourcePath, f.MapValue.EnumTypeName)
			}
		}
		for _, o := range m.Oneofs {
			for _, v := range o.Variants {
				if v.SourcePath != "" {
					addRef(v.SourcePath, v.TypeName)
				}
			}
		}
	}

	if len(needed) == 0 {
		return nil
	}

	// Convert to sorted import entries.
	var entries []tsImportEntry
	for protoPath, nameSet := range needed {
		// Map proto path to relative .type.js import path.
		relPath := tsImportPath(ir.SourcePath, protoPath, opts)
		var names []string
		for n := range nameSet {
			names = append(names, n)
		}
		sort.Strings(names)
		entries = append(entries, tsImportEntry{path: relPath, names: names})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].path < entries[j].path
	})
	return entries
}

// tsImportPath computes the relative ESM import path from the current proto
// file to the referenced proto file, using the configured extension.
func tsImportPath(fromProto, toProto string, opts *Options) string {
	fromDir := path.Dir(fromProto)

	// Default to .type.ts if suffix is not configured
	suffix := opts.TSOutputSuffix
	if suffix == "" {
		suffix = ".type.ts"
	}
	// For ESM imports, .ts becomes .js
	importSuffix := strings.TrimSuffix(suffix, ".ts") + ".js"

	// Strip .proto extension, add import suffix.
	toBase := strings.TrimSuffix(toProto, ".proto") + importSuffix

	// Compute relative path using POSIX conventions (proto paths are always forward-slash).
	// Split both into components and find common prefix.
	fromParts := strings.Split(fromDir, "/")
	toParts := strings.Split(toBase, "/")

	if fromDir == "." {
		fromParts = nil
	}

	// Find common prefix length.
	common := 0
	for common < len(fromParts) && common < len(toParts) && fromParts[common] == toParts[common] {
		common++
	}

	// Build relative path: go up for remaining fromParts, then down for remaining toParts.
	var relParts []string
	for i := common; i < len(fromParts); i++ {
		relParts = append(relParts, "..")
	}
	relParts = append(relParts, toParts[common:]...)

	rel := strings.Join(relParts, "/")
	if !strings.HasPrefix(rel, ".") {
		rel = "./" + rel
	}
	// Security: Prevent excessive path traversal (H5).
	// Note: protoc itself largely controls input paths, but a crafted source_path
	// could theoretically escape the output directory.
	if strings.HasPrefix(rel, "../../") {
		// Just use the basename to be safe if it attempts to escape project root.
		rel = "./" + toParts[len(toParts)-1]
	}
	return rel
}
