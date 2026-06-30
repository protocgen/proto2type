package generator

import (
	"fmt"

	"google.golang.org/protobuf/compiler/protogen"
)

// GenerateFile generates output files for a single proto file.
func GenerateFile(gen *protogen.Plugin, file *protogen.File, opts *Options) error {
	if len(file.Messages) == 0 {
		return nil
	}

	switch opts.Lang {
	case "go", "":
		return generateGo(gen, file, opts)
	case "rust":
		return generateRust(gen, file, opts)
	case "kotlin":
		return generateKotlin(gen, file, opts)
	default:
		return fmt.Errorf("proto2type: unsupported language %q (supported: go, rust, kotlin)", opts.Lang)
	}
}
