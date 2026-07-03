package generator

import (
	"fmt"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	descriptorpb "google.golang.org/protobuf/types/descriptorpb"

	validate "buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate"
)

// extractValidateConstraints reads buf/validate field-level constraints from
// the (buf.validate.field) extension on a proto field. Returns nil if no
// constraints are present.
func extractValidateConstraints(field *protogen.Field) *ValidateConstraints {
	opts, ok := field.Desc.Options().(*descriptorpb.FieldOptions)
	if !ok || opts == nil {
		return nil
	}
	if !proto.HasExtension(opts, validate.E_Field) {
		return nil
	}
	rules, ok := proto.GetExtension(opts, validate.E_Field).(*validate.FieldRules)
	if !ok || rules == nil {
		return nil
	}

	vc := &ValidateConstraints{
		Required: rules.GetRequired(),
	}

	// String rules.
	if sr := rules.GetString(); sr != nil {
		if sr.MinLen != nil {
			v := *sr.MinLen
			vc.MinLength = &v
		}
		if sr.MaxLen != nil {
			v := *sr.MaxLen
			vc.MaxLength = &v
		}
		vc.Pattern = sr.GetPattern()

		// Well-known string types are a oneof — use type assertions.
		switch sr.GetWellKnown().(type) {
		case *validate.StringRules_Email:
			vc.Email = true
		case *validate.StringRules_Uuid:
			vc.UUID = true
		case *validate.StringRules_Uri:
			vc.URI = true
		}
	}

	// Bytes rules.
	if br := rules.GetBytes(); br != nil {
		if br.MinLen != nil {
			v := *br.MinLen
			vc.MinLength = &v
		}
		if br.MaxLen != nil {
			v := *br.MaxLen
			vc.MaxLength = &v
		}
	}

	// Int32 rules.
	if ir := rules.GetInt32(); ir != nil {
		extractInt32Bounds(ir, vc)
	}

	// Int64 rules.
	if ir := rules.GetInt64(); ir != nil {
		extractInt64Bounds(ir, vc)
	}

	// UInt32 rules.
	if ur := rules.GetUint32(); ur != nil {
		extractUInt32Bounds(ur, vc)
	}

	// Float rules.
	if fr := rules.GetFloat(); fr != nil {
		extractFloatBounds(fr, vc)
	}

	// Double rules.
	if dr := rules.GetDouble(); dr != nil {
		extractDoubleBounds(dr, vc)
	}

	// Repeated rules.
	if rr := rules.GetRepeated(); rr != nil {
		if rr.MinItems != nil {
			v := *rr.MinItems
			vc.MinItems = &v
		}
		if rr.MaxItems != nil {
			v := *rr.MaxItems
			vc.MaxItems = &v
		}
	}

	if !vc.HasConstraints() {
		return nil
	}
	return vc
}

// extractInt32Bounds reads GreaterThan/LessThan oneof from Int32Rules.
func extractInt32Bounds(r *validate.Int32Rules, vc *ValidateConstraints) {
	switch gt := r.GetGreaterThan().(type) {
	case *validate.Int32Rules_Gt:
		s := fmt.Sprintf("%d", gt.Gt)
		vc.Gt = &s
	case *validate.Int32Rules_Gte:
		s := fmt.Sprintf("%d", gt.Gte)
		vc.Gte = &s
	}
	switch lt := r.GetLessThan().(type) {
	case *validate.Int32Rules_Lt:
		s := fmt.Sprintf("%d", lt.Lt)
		vc.Lt = &s
	case *validate.Int32Rules_Lte:
		s := fmt.Sprintf("%d", lt.Lte)
		vc.Lte = &s
	}
}

// extractInt64Bounds reads GreaterThan/LessThan oneof from Int64Rules.
func extractInt64Bounds(r *validate.Int64Rules, vc *ValidateConstraints) {
	switch gt := r.GetGreaterThan().(type) {
	case *validate.Int64Rules_Gt:
		s := fmt.Sprintf("%d", gt.Gt)
		vc.Gt = &s
	case *validate.Int64Rules_Gte:
		s := fmt.Sprintf("%d", gt.Gte)
		vc.Gte = &s
	}
	switch lt := r.GetLessThan().(type) {
	case *validate.Int64Rules_Lt:
		s := fmt.Sprintf("%d", lt.Lt)
		vc.Lt = &s
	case *validate.Int64Rules_Lte:
		s := fmt.Sprintf("%d", lt.Lte)
		vc.Lte = &s
	}
}

// extractUInt32Bounds reads GreaterThan/LessThan oneof from UInt32Rules.
func extractUInt32Bounds(r *validate.UInt32Rules, vc *ValidateConstraints) {
	switch gt := r.GetGreaterThan().(type) {
	case *validate.UInt32Rules_Gt:
		s := fmt.Sprintf("%d", gt.Gt)
		vc.Gt = &s
	case *validate.UInt32Rules_Gte:
		s := fmt.Sprintf("%d", gt.Gte)
		vc.Gte = &s
	}
	switch lt := r.GetLessThan().(type) {
	case *validate.UInt32Rules_Lt:
		s := fmt.Sprintf("%d", lt.Lt)
		vc.Lt = &s
	case *validate.UInt32Rules_Lte:
		s := fmt.Sprintf("%d", lt.Lte)
		vc.Lte = &s
	}
}

// extractFloatBounds reads GreaterThan/LessThan oneof from FloatRules.
func extractFloatBounds(r *validate.FloatRules, vc *ValidateConstraints) {
	switch gt := r.GetGreaterThan().(type) {
	case *validate.FloatRules_Gt:
		s := fmt.Sprintf("%g", gt.Gt)
		vc.Gt = &s
	case *validate.FloatRules_Gte:
		s := fmt.Sprintf("%g", gt.Gte)
		vc.Gte = &s
	}
	switch lt := r.GetLessThan().(type) {
	case *validate.FloatRules_Lt:
		s := fmt.Sprintf("%g", lt.Lt)
		vc.Lt = &s
	case *validate.FloatRules_Lte:
		s := fmt.Sprintf("%g", lt.Lte)
		vc.Lte = &s
	}
}

// extractDoubleBounds reads GreaterThan/LessThan oneof from DoubleRules.
func extractDoubleBounds(r *validate.DoubleRules, vc *ValidateConstraints) {
	switch gt := r.GetGreaterThan().(type) {
	case *validate.DoubleRules_Gt:
		s := fmt.Sprintf("%g", gt.Gt)
		vc.Gt = &s
	case *validate.DoubleRules_Gte:
		s := fmt.Sprintf("%g", gt.Gte)
		vc.Gte = &s
	}
	switch lt := r.GetLessThan().(type) {
	case *validate.DoubleRules_Lt:
		s := fmt.Sprintf("%g", lt.Lt)
		vc.Lt = &s
	case *validate.DoubleRules_Lte:
		s := fmt.Sprintf("%g", lt.Lte)
		vc.Lte = &s
	}
}
