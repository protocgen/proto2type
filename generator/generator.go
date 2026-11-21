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

// Generate processes all marked proto files in gen, consolidating output into a
// single file when opts.OutputFile specifies a valid single-file target.
func (r *Runner) Generate(gen *protogen.Plugin, opts *Options) error {
	if opts == nil {
		opts = &Options{}
	}
	var genFiles []*protogen.File
	for _, f := range gen.Files {
		if f.Generate {
			genFiles = append(genFiles, f)
		}
	}
	if err := ValidateOutputFileOptions(opts, len(genFiles)); err != nil {
		return err
	}
	if opts.OutputFile == "" || len(genFiles) <= 1 {
		for _, f := range genFiles {
			if err := r.GenerateFile(gen, f, opts); err != nil {
				return err
			}
		}
		return nil
	}

	for _, f := range genFiles {
		if len(f.Messages) == 0 && len(f.Enums) == 0 {
			continue
		}
		if f.Desc.Syntax() == protoreflect.Proto2 {
			return fmt.Errorf("proto2type: %s uses proto2 syntax, only proto3 (and editions) are supported", f.Desc.Path())
		}
	}

	if !opts.Domain && (opts.Lang == "go" || opts.Lang == "") {
		switch opts.Backend {
		case "postgres":
			return r.goGen.generateGoPostgresConsolidated(gen, genFiles, opts)
		case "doris", "starrocks":
			return r.goGen.generateGoDorisConsolidated(gen, genFiles, opts)
		}
	}

	for _, f := range genFiles {
		if err := r.GenerateFile(gen, f, opts); err != nil {
			return err
		}
	}
	return nil
}

// GenerateFile generates output files for a single proto file.
func (r *Runner) GenerateFile(gen *protogen.Plugin, file *protogen.File, opts *Options) error {
	if opts != nil && opts.OutputFile != "" {
		if err := ValidateOutputFileOptions(opts, 1); err != nil {
			return err
		}
	}
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
