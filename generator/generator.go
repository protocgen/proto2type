package generator

import (
	"fmt"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// irValidationError is a typed panic value used by IR validation checks
// (nesting depth, name collisions) so that GenerateFile can recover them
// cleanly without masking unrelated panics (nil deref, index OOB).
type irValidationError struct {
	msg string
}

func (e irValidationError) Error() string { return e.msg }

// irPanic raises an irValidationError panic that GenerateFile will recover
// and convert to a proper protoc error.
func irPanic(format string, args ...any) {
	panic(irValidationError{msg: fmt.Sprintf(format, args...)})
}

// GenerateFile generates output files for a single proto file.
func GenerateFile(gen *protogen.Plugin, file *protogen.File, opts *Options) (retErr error) {
	// Recover IR validation panics and convert them to errors so protoc
	// displays a clean message. Unrelated panics (real bugs) re-panic
	// with their original value and stack trace.
	defer func() {
		if r := recover(); r != nil {
			if ve, ok := r.(irValidationError); ok {
				retErr = ve
			} else {
				panic(r) // re-panic: not an IR validation error
			}
		}
	}()
	if len(file.Messages) == 0 {
		return nil
	}

	// Reject proto2 — optional/required/group semantics differ and produce
	// silently incorrect output (ARCH-4).
	if file.Desc.Syntax() == protoreflect.Proto2 {
		return fmt.Errorf("proto2type: %s uses proto2 syntax, only proto3 (and editions) are supported", file.Desc.Path())
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
