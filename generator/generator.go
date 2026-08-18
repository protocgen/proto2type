package generator

import (
	"fmt"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Runner holds per-invocation state for the code generator.
type Runner struct {
	goGen *goGenerator
}

// NewRunner creates a new Runner.
func NewRunner() *Runner {
	return &Runner{
		goGen: newGoGenerator(),
	}
}

// GenerateFile generates output files for a single proto file.
func (r *Runner) GenerateFile(gen *protogen.Plugin, file *protogen.File, opts *Options) error {
	if len(file.Messages) == 0 && len(file.Enums) == 0 {
		return nil
	}

	// Reject proto2 — optional/required/group semantics differ and produce
	// silently incorrect output (ARCH-4).
	if file.Desc.Syntax() == protoreflect.Proto2 {
		return fmt.Errorf("proto2type: %s uses proto2 syntax, only proto3 (and editions) are supported", file.Desc.Path())
	}

	switch opts.Lang {
	case "go", "":
		return r.goGen.generateGo(gen, file, opts)
	case "rust":
		return generateRust(gen, file, opts)
	case "kotlin":
		return generateKotlin(gen, file, opts)
	case "python":
		return generatePython(gen, file, opts)
	case "typescript":
		return generateTypeScript(gen, file, opts)
	default:
		return fmt.Errorf("proto2type: unsupported language %q (supported: go, rust, kotlin, python, typescript)", opts.Lang)
	}
}
