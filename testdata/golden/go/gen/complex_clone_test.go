package gen

import (
	"testing"
)

// TestWktPayloadCloneNilInnerValue is a regression test for issue #73.
// When a oneof variant has a non-nil pointer to a nil inner value,
// Clone() must preserve nil (not convert to empty).
func TestWktPayloadCloneNilInnerValue(t *testing.T) {
	tests := []struct {
		name string
		orig *WktPayload
	}{
		{
			name: "nil struct (map[string]any)",
			orig: func() *WktPayload {
				var m map[string]any // nil map
				return &WktPayload{ID: "s1", StructData: &m}
			}(),
		},
		{
			name: "nil value (any)",
			orig: func() *WktPayload {
				var v any // nil interface
				return &WktPayload{ID: "v1", AnyValue: &v}
			}(),
		},
		{
			name: "nil list ([]any)",
			orig: func() *WktPayload {
				var l []any // nil slice
				return &WktPayload{ID: "l1", ListData: &l}
			}(),
		},
		{
			name: "nil mask ([]string)",
			orig: func() *WktPayload {
				var m []string // nil slice
				return &WktPayload{ID: "m1", Mask: &m}
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cloned := tt.orig.Clone()
			if !cloned.Equal(tt.orig) {
				t.Errorf("Clone().Equal(original) = false; nil inner value was converted to empty")
			}
		})
	}
}

// TestWktPayloadClonePopulatedValues verifies Clone deep-copies
// populated WKT oneof variants (the non-nil inner value path).
func TestWktPayloadClonePopulatedValues(t *testing.T) {
	structData := map[string]any{"key": "value"}
	anyVal := any("hello")
	listData := []any{1.0, "two", true}
	mask := []string{"field_a", "field_b"}

	tests := []struct {
		name string
		orig *WktPayload
	}{
		{
			name: "populated struct",
			orig: &WktPayload{ID: "s2", StructData: &structData},
		},
		{
			name: "populated value",
			orig: &WktPayload{ID: "v2", AnyValue: &anyVal},
		},
		{
			name: "populated list",
			orig: &WktPayload{ID: "l2", ListData: &listData},
		},
		{
			name: "populated mask",
			orig: &WktPayload{ID: "m2", Mask: &mask},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cloned := tt.orig.Clone()
			if !cloned.Equal(tt.orig) {
				t.Errorf("Clone().Equal(original) = false for populated value")
			}
		})
	}
}

// TestWktPayloadCloneNilReceiver ensures Clone of nil returns nil.
func TestWktPayloadCloneNilReceiver(t *testing.T) {
	var w *WktPayload
	if w.Clone() != nil {
		t.Error("Clone of nil WktPayload should return nil")
	}
}
