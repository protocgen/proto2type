package integration

import (
	"testing"

	"github.com/protocgen/proto2type/testdata/golden/go/gen"
)

// ---------- EmptyMessage ----------

func TestEdgeCase_EmptyMessage(t *testing.T) {
	t.Run("Clone", func(t *testing.T) {
		e := &gen.EmptyMessage{}
		clone := e.Clone()
		if clone == nil {
			t.Fatal("Clone returned nil")
		}
		if !clone.Equal(e) {
			t.Fatal("Clone not Equal")
		}
	})

	t.Run("Equal_self", func(t *testing.T) {
		e := &gen.EmptyMessage{}
		if !e.Equal(e) {
			t.Fatal("not reflexive")
		}
	})

	t.Run("ProtoRoundtrip", func(t *testing.T) {
		e := &gen.EmptyMessage{}
		pb := e.ToProto()
		restored := &gen.EmptyMessage{}
		restored.FromProto(pb)
		if !restored.Equal(e) {
			t.Fatal("roundtrip mismatch")
		}
	})

	t.Run("Nil_safety", func(t *testing.T) {
		var e *gen.EmptyMessage
		if e.Clone() != nil {
			t.Fatal("nil.Clone() should be nil")
		}
		if e.ToProto() != nil {
			t.Fatal("nil.ToProto() should be nil")
		}
	})
}

// ---------- AllOptionalScalars ----------

func TestEdgeCase_AllOptionalScalars(t *testing.T) {
	t.Run("AllNil", func(t *testing.T) {
		a := &gen.AllOptionalScalars{}
		clone := a.Clone()
		if !clone.Equal(a) {
			t.Fatal("all-nil Clone/Equal failed")
		}
		pb := a.ToProto()
		restored := &gen.AllOptionalScalars{}
		restored.FromProto(pb)
		if !restored.Equal(a) {
			t.Fatal("all-nil roundtrip failed")
		}
	})

	t.Run("AllSet", func(t *testing.T) {
		s := "hello"
		i32 := int32(42)
		i64 := int64(9999)
		b := true
		d := 3.14
		f := float32(2.71)
		bs := []byte{0xDE, 0xAD}
		e := int32(0) // SingleValueEnum UNSPECIFIED

		a := &gen.AllOptionalScalars{
			OptString: &s,
			OptInt32:  &i32,
			OptInt64:  &i64,
			OptBool:   &b,
			OptDouble: &d,
			OptFloat:  &f,
			OptBytes:  &bs,
			OptEnum:   &e,
		}
		clone := a.Clone()
		if !clone.Equal(a) {
			t.Fatal("all-set Clone/Equal failed")
		}

		// Verify clone independence
		*clone.OptString = "mutated"
		*clone.OptInt32 = 0
		if a.Equal(clone) {
			t.Fatal("mutation leaked to original")
		}

		// Proto roundtrip
		pb := a.ToProto()
		restored := &gen.AllOptionalScalars{}
		restored.FromProto(pb)
		if !restored.Equal(a) {
			t.Fatalf("roundtrip mismatch:\n  original: %+v\n  restored: %+v", a, restored)
		}
	})
}

// ---------- OneofOnly ----------

func TestEdgeCase_OneofOnly(t *testing.T) {
	t.Run("NoVariantSet", func(t *testing.T) {
		o := &gen.OneofOnly{}
		clone := o.Clone()
		if !clone.Equal(o) {
			t.Fatal("empty Clone/Equal failed")
		}
		pb := o.ToProto()
		restored := &gen.OneofOnly{}
		restored.FromProto(pb)
		if !restored.Equal(o) {
			t.Fatal("empty roundtrip failed")
		}
	})

	t.Run("TextVariant", func(t *testing.T) {
		s := "hello"
		o := &gen.OneofOnly{Text: &s}
		clone := o.Clone()
		if !clone.Equal(o) {
			t.Fatal("text Clone/Equal failed")
		}
		// Independence
		*clone.Text = "mutated"
		if o.Equal(clone) {
			t.Fatal("mutation leaked")
		}
		// Roundtrip
		pb := o.ToProto()
		restored := &gen.OneofOnly{}
		restored.FromProto(pb)
		if !restored.Equal(o) {
			t.Fatal("text roundtrip failed")
		}
	})

	t.Run("NumberVariant", func(t *testing.T) {
		n := int32(42)
		o := &gen.OneofOnly{Number: &n}
		pb := o.ToProto()
		restored := &gen.OneofOnly{}
		restored.FromProto(pb)
		if !restored.Equal(o) {
			t.Fatal("number roundtrip failed")
		}
	})

	t.Run("FlagVariant", func(t *testing.T) {
		b := true
		o := &gen.OneofOnly{Flag: &b}
		pb := o.ToProto()
		restored := &gen.OneofOnly{}
		restored.FromProto(pb)
		if !restored.Equal(o) {
			t.Fatal("flag roundtrip failed")
		}
	})

	t.Run("ReceiverReuse_ClearsStaleVariant", func(t *testing.T) {
		s := "hello"
		n := int32(42)

		o := &gen.OneofOnly{}
		pbText := (&gen.OneofOnly{Text: &s}).ToProto()
		pbNumber := (&gen.OneofOnly{Number: &n}).ToProto()

		o.FromProto(pbText)
		if o.Text == nil || *o.Text != "hello" {
			t.Fatal("text variant not set")
		}

		o.FromProto(pbNumber)
		if o.Text != nil {
			t.Fatal("stale text variant not cleared on receiver reuse")
		}
		if o.Number == nil || *o.Number != 42 {
			t.Fatal("number variant not set")
		}
	})
}

// ---------- MapOnly ----------

func TestEdgeCase_MapOnly(t *testing.T) {
	t.Run("Empty", func(t *testing.T) {
		m := &gen.MapOnly{}
		clone := m.Clone()
		if !clone.Equal(m) {
			t.Fatal("empty Clone/Equal failed")
		}
		pb := m.ToProto()
		restored := &gen.MapOnly{}
		restored.FromProto(pb)
		if !restored.Equal(m) {
			t.Fatal("empty roundtrip failed")
		}
	})

	t.Run("Populated", func(t *testing.T) {
		m := &gen.MapOnly{
			StringMap: map[string]string{"a": "b", "c": "d"},
			IntMap:    map[int32]string{1: "one", 2: "two"},
		}
		clone := m.Clone()
		if !clone.Equal(m) {
			t.Fatal("populated Clone/Equal failed")
		}
		// Independence
		clone.StringMap["a"] = "mutated"
		if m.Equal(clone) {
			t.Fatal("map mutation leaked")
		}
		// Roundtrip
		pb := m.ToProto()
		restored := &gen.MapOnly{}
		restored.FromProto(pb)
		if !restored.Equal(m) {
			t.Fatal("populated roundtrip failed")
		}
	})
}

// ---------- RepeatedOnly ----------

func TestEdgeCase_RepeatedOnly(t *testing.T) {
	t.Run("Empty", func(t *testing.T) {
		r := &gen.RepeatedOnly{}
		clone := r.Clone()
		if !clone.Equal(r) {
			t.Fatal("empty Clone/Equal failed")
		}
		pb := r.ToProto()
		restored := &gen.RepeatedOnly{}
		restored.FromProto(pb)
		if !restored.Equal(r) {
			t.Fatal("empty roundtrip failed")
		}
	})

	t.Run("Populated", func(t *testing.T) {
		r := &gen.RepeatedOnly{
			Items:   []string{"a", "b", "c"},
			Numbers: []int32{1, 2, 3},
		}
		clone := r.Clone()
		if !clone.Equal(r) {
			t.Fatal("populated Clone/Equal failed")
		}
		// Independence
		clone.Items[0] = "mutated"
		clone.Numbers[0] = 999
		if r.Equal(clone) {
			t.Fatal("slice mutation leaked")
		}
		// Roundtrip
		pb := r.ToProto()
		restored := &gen.RepeatedOnly{}
		restored.FromProto(pb)
		if !restored.Equal(r) {
			t.Fatal("populated roundtrip failed")
		}
	})
}
