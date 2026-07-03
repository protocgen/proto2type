package generator

import (
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestEnsurePythonTrailingPeriod(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Hello", "Hello."},
		{"Hello.", "Hello."},
		{"Hello?", "Hello?"},
		{"Hello!", "Hello!"},
		{"", ""},
		{"A sentence", "A sentence."},
		{"Already done.", "Already done."},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ensurePythonTrailingPeriod(tt.input)
			if got != tt.want {
				t.Errorf("ensurePythonTrailingPeriod(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestEscapePythonString(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain", "hello", "hello"},
		{"single quote", "it's", "it\\'s"},
		{"newline replaced", "line1\nline2", "line1 line2"},
		{"backslash escaped", "back\\slash", "back\\\\slash"},
		{"double quote", `say "hi"`, `say \"hi\"`},
		{"carriage return removed", "line1\rline2", "line1line2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := escapePythonString(tt.input)
			if got != tt.want {
				t.Errorf("escapePythonString(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestTopologicalSortMessages(t *testing.T) {
	t.Run("empty list", func(t *testing.T) {
		result := topologicalSortMessages(nil)
		if len(result) != 0 {
			t.Errorf("expected empty, got %d", len(result))
		}
	})

	t.Run("single message", func(t *testing.T) {
		msgs := []*DomainMessage{{Name: "A"}}
		result := topologicalSortMessages(msgs)
		if len(result) != 1 || result[0].Name != "A" {
			t.Errorf("expected [A], got %v", msgNames(result))
		}
	})

	t.Run("A depends on B", func(t *testing.T) {
		msgs := []*DomainMessage{
			{Name: "A", Fields: []*DomainField{{MessageTypeName: "B"}}},
			{Name: "B"},
		}
		result := topologicalSortMessages(msgs)
		names := msgNames(result)
		idxA := indexOf(names, "A")
		idxB := indexOf(names, "B")
		if idxB >= idxA {
			t.Errorf("B should come before A, got %v", names)
		}
	})

	t.Run("no dependencies preserves order", func(t *testing.T) {
		msgs := []*DomainMessage{
			{Name: "X"},
			{Name: "Y"},
			{Name: "Z"},
		}
		result := topologicalSortMessages(msgs)
		names := msgNames(result)
		if names[0] != "X" || names[1] != "Y" || names[2] != "Z" {
			t.Errorf("expected [X Y Z], got %v", names)
		}
	})

	t.Run("diamond dependency", func(t *testing.T) {
		// A→B, A→C, B→D, C→D
		msgs := []*DomainMessage{
			{Name: "A", Fields: []*DomainField{
				{MessageTypeName: "B"},
				{MessageTypeName: "C"},
			}},
			{Name: "B", Fields: []*DomainField{{MessageTypeName: "D"}}},
			{Name: "C", Fields: []*DomainField{{MessageTypeName: "D"}}},
			{Name: "D"},
		}
		result := topologicalSortMessages(msgs)
		names := msgNames(result)
		idxA := indexOf(names, "A")
		idxB := indexOf(names, "B")
		idxC := indexOf(names, "C")
		idxD := indexOf(names, "D")
		if idxD >= idxB {
			t.Errorf("D should come before B, got %v", names)
		}
		if idxD >= idxC {
			t.Errorf("D should come before C, got %v", names)
		}
		if idxB >= idxA {
			t.Errorf("B should come before A, got %v", names)
		}
		if idxC >= idxA {
			t.Errorf("C should come before A, got %v", names)
		}
	})
}

func TestPythonOneofUnionType(t *testing.T) {
	opts := &Options{Lang: "python"}

	tests := []struct {
		name  string
		oneof DomainOneof
		want  string
	}{
		{
			name: "two scalar variants",
			oneof: DomainOneof{
				Variants: []*OneofVariant{
					{Kind: FieldKindScalar, ScalarKind: protoreflect.StringKind},
					{Kind: FieldKindScalar, ScalarKind: protoreflect.Int32Kind},
				},
			},
			want: "str | int | None",
		},
		{
			name: "message and scalar",
			oneof: DomainOneof{
				Variants: []*OneofVariant{
					{Kind: FieldKindMessage, TypeName: "MyMsg"},
					{Kind: FieldKindScalar, ScalarKind: protoreflect.StringKind},
				},
			},
			want: "MyMsg | str | None",
		},
		{
			name: "duplicate types deduped",
			oneof: DomainOneof{
				Variants: []*OneofVariant{
					{Kind: FieldKindScalar, ScalarKind: protoreflect.StringKind},
					{Kind: FieldKindScalar, ScalarKind: protoreflect.StringKind},
				},
			},
			want: "str | None",
		},
		{
			name: "timestamp variant",
			oneof: DomainOneof{
				Variants: []*OneofVariant{
					{Kind: FieldKindTimestamp},
				},
			},
			want: "datetime | None",
		},
		{
			name: "duration variant",
			oneof: DomainOneof{
				Variants: []*OneofVariant{
					{Kind: FieldKindDuration},
				},
			},
			want: "timedelta | None",
		},
		{
			name: "struct variant",
			oneof: DomainOneof{
				Variants: []*OneofVariant{
					{Kind: FieldKindStruct},
				},
			},
			want: "dict[str, Any] | None",
		},
		{
			name: "enum variant",
			oneof: DomainOneof{
				Variants: []*OneofVariant{
					{Kind: FieldKindEnum, TypeName: "Status"},
				},
			},
			want: "Status | None",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pythonOneofUnionType(&tt.oneof, opts)
			if got != tt.want {
				t.Errorf("pythonOneofUnionType() = %q, want %q", got, tt.want)
			}
		})
	}
}

// --- helpers ---

func msgNames(msgs []*DomainMessage) []string {
	names := make([]string, len(msgs))
	for i, m := range msgs {
		names[i] = m.Name
	}
	return names
}

func indexOf(names []string, target string) int {
	for i, n := range names {
		if n == target {
			return i
		}
	}
	return -1
}
