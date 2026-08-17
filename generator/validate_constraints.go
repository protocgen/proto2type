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
		Required:    rules.GetRequired(),
		IgnoreEmpty: rules.GetIgnore() == validate.Ignore_IGNORE_IF_ZERO_VALUE,
	}

	// String rules.
	if sr := rules.GetString(); sr != nil {
		if sr.MinLen != nil {
			v := sr.GetMinLen()
			vc.MinLength = &v
		}
		if sr.MaxLen != nil {
			v := sr.GetMaxLen()
			vc.MaxLength = &v
		}
		if sr.Len != nil {
			v := sr.GetLen()
			vc.Len = &v
		}
		vc.Pattern = sr.GetPattern()
		vc.Prefix = sr.GetPrefix()
		vc.Suffix = sr.GetSuffix()
		vc.Contains = sr.GetContains()

		// Well-known string types are a oneof — use type assertions.
		switch sr.GetWellKnown().(type) {
		case *validate.StringRules_Email:
			vc.Email = true
		case *validate.StringRules_Uuid:
			vc.UUID = true
		case *validate.StringRules_Uri:
			vc.URI = true
		case *validate.StringRules_Hostname:
			vc.Hostname = true
		case *validate.StringRules_Ip:
			vc.IP = true
		}
	}

	// Bytes rules.
	if br := rules.GetBytes(); br != nil {
		if br.MinLen != nil {
			v := br.GetMinLen()
			vc.MinLength = &v
		}
		if br.MaxLen != nil {
			v := br.GetMaxLen()
			vc.MaxLength = &v
		}
		if br.Len != nil {
			v := br.GetLen()
			vc.Len = &v
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

	// UInt64 rules.
	if ur := rules.GetUint64(); ur != nil {
		extractUInt64Bounds(ur, vc)
	}

	// SInt32 rules.
	if sr := rules.GetSint32(); sr != nil {
		extractSInt32Bounds(sr, vc)
	}

	// SInt64 rules.
	if sr := rules.GetSint64(); sr != nil {
		extractSInt64Bounds(sr, vc)
	}

	// Fixed32 rules.
	if fr := rules.GetFixed32(); fr != nil {
		extractFixed32Bounds(fr, vc)
	}

	// Fixed64 rules.
	if fr := rules.GetFixed64(); fr != nil {
		extractFixed64Bounds(fr, vc)
	}

	// SFixed32 rules.
	if sr := rules.GetSfixed32(); sr != nil {
		extractSFixed32Bounds(sr, vc)
	}

	// SFixed64 rules.
	if sr := rules.GetSfixed64(); sr != nil {
		extractSFixed64Bounds(sr, vc)
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
		vc.Unique = rr.GetUnique()
		if rr.MinItems != nil {
			v := rr.GetMinItems()
			vc.MinItems = &v
		}
		if rr.MaxItems != nil {
			v := rr.GetMaxItems()
			vc.MaxItems = &v
		}
	}

	// Map rules (min_pairs/max_pairs → MinItems/MaxItems).
	if mr := rules.GetMap(); mr != nil {
		if mr.MinPairs != nil {
			v := mr.GetMinPairs()
			vc.MinItems = &v
		}
		if mr.MaxPairs != nil {
			v := mr.GetMaxPairs()
			vc.MaxItems = &v
		}
	}

	// Enum rules.
	if er := rules.GetEnum(); er != nil {
		vc.DefinedOnly = er.GetDefinedOnly()
		for _, v := range er.GetIn() {
			vc.In = append(vc.In, fmt.Sprintf("%d", v))
		}
		for _, v := range er.GetNotIn() {
			vc.NotIn = append(vc.NotIn, fmt.Sprintf("%d", v))
		}
	}

	if !vc.HasConstraints() {
		return nil
	}
	return vc
}

// extractInt32Bounds reads GreaterThan/LessThan oneof from Int32Rules.
func extractInt32Bounds(r *validate.Int32Rules, vc *ValidateConstraints) {
	if r.Const != nil {
		s := fmt.Sprintf("%d", r.GetConst())
		vc.Const = &s
	}
	for _, v := range r.GetIn() {
		vc.In = append(vc.In, fmt.Sprintf("%d", v))
	}
	for _, v := range r.GetNotIn() {
		vc.NotIn = append(vc.NotIn, fmt.Sprintf("%d", v))
	}
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
	if r.Const != nil {
		s := fmt.Sprintf("%d", r.GetConst())
		vc.Const = &s
	}
	for _, v := range r.GetIn() {
		vc.In = append(vc.In, fmt.Sprintf("%d", v))
	}
	for _, v := range r.GetNotIn() {
		vc.NotIn = append(vc.NotIn, fmt.Sprintf("%d", v))
	}
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
	if r.Const != nil {
		s := fmt.Sprintf("%d", r.GetConst())
		vc.Const = &s
	}
	for _, v := range r.GetIn() {
		vc.In = append(vc.In, fmt.Sprintf("%d", v))
	}
	for _, v := range r.GetNotIn() {
		vc.NotIn = append(vc.NotIn, fmt.Sprintf("%d", v))
	}
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
	if r.Const != nil {
		s := fmt.Sprintf("%g", r.GetConst())
		vc.Const = &s
	}
	for _, v := range r.GetIn() {
		vc.In = append(vc.In, fmt.Sprintf("%g", v))
	}
	for _, v := range r.GetNotIn() {
		vc.NotIn = append(vc.NotIn, fmt.Sprintf("%g", v))
	}
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
	if r.Const != nil {
		s := fmt.Sprintf("%g", r.GetConst())
		vc.Const = &s
	}
	for _, v := range r.GetIn() {
		vc.In = append(vc.In, fmt.Sprintf("%g", v))
	}
	for _, v := range r.GetNotIn() {
		vc.NotIn = append(vc.NotIn, fmt.Sprintf("%g", v))
	}
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

// extractUInt64Bounds reads GreaterThan/LessThan oneof from UInt64Rules.
func extractUInt64Bounds(r *validate.UInt64Rules, vc *ValidateConstraints) {
	if r.Const != nil {
		s := fmt.Sprintf("%d", r.GetConst())
		vc.Const = &s
	}
	for _, v := range r.GetIn() {
		vc.In = append(vc.In, fmt.Sprintf("%d", v))
	}
	for _, v := range r.GetNotIn() {
		vc.NotIn = append(vc.NotIn, fmt.Sprintf("%d", v))
	}
	switch gt := r.GetGreaterThan().(type) {
	case *validate.UInt64Rules_Gt:
		s := fmt.Sprintf("%d", gt.Gt)
		vc.Gt = &s
	case *validate.UInt64Rules_Gte:
		s := fmt.Sprintf("%d", gt.Gte)
		vc.Gte = &s
	}
	switch lt := r.GetLessThan().(type) {
	case *validate.UInt64Rules_Lt:
		s := fmt.Sprintf("%d", lt.Lt)
		vc.Lt = &s
	case *validate.UInt64Rules_Lte:
		s := fmt.Sprintf("%d", lt.Lte)
		vc.Lte = &s
	}
}

// extractSInt32Bounds reads GreaterThan/LessThan oneof from SInt32Rules.
func extractSInt32Bounds(r *validate.SInt32Rules, vc *ValidateConstraints) {
	if r.Const != nil {
		s := fmt.Sprintf("%d", r.GetConst())
		vc.Const = &s
	}
	for _, v := range r.GetIn() {
		vc.In = append(vc.In, fmt.Sprintf("%d", v))
	}
	for _, v := range r.GetNotIn() {
		vc.NotIn = append(vc.NotIn, fmt.Sprintf("%d", v))
	}
	switch gt := r.GetGreaterThan().(type) {
	case *validate.SInt32Rules_Gt:
		s := fmt.Sprintf("%d", gt.Gt)
		vc.Gt = &s
	case *validate.SInt32Rules_Gte:
		s := fmt.Sprintf("%d", gt.Gte)
		vc.Gte = &s
	}
	switch lt := r.GetLessThan().(type) {
	case *validate.SInt32Rules_Lt:
		s := fmt.Sprintf("%d", lt.Lt)
		vc.Lt = &s
	case *validate.SInt32Rules_Lte:
		s := fmt.Sprintf("%d", lt.Lte)
		vc.Lte = &s
	}
}

// extractSInt64Bounds reads GreaterThan/LessThan oneof from SInt64Rules.
func extractSInt64Bounds(r *validate.SInt64Rules, vc *ValidateConstraints) {
	if r.Const != nil {
		s := fmt.Sprintf("%d", r.GetConst())
		vc.Const = &s
	}
	for _, v := range r.GetIn() {
		vc.In = append(vc.In, fmt.Sprintf("%d", v))
	}
	for _, v := range r.GetNotIn() {
		vc.NotIn = append(vc.NotIn, fmt.Sprintf("%d", v))
	}
	switch gt := r.GetGreaterThan().(type) {
	case *validate.SInt64Rules_Gt:
		s := fmt.Sprintf("%d", gt.Gt)
		vc.Gt = &s
	case *validate.SInt64Rules_Gte:
		s := fmt.Sprintf("%d", gt.Gte)
		vc.Gte = &s
	}
	switch lt := r.GetLessThan().(type) {
	case *validate.SInt64Rules_Lt:
		s := fmt.Sprintf("%d", lt.Lt)
		vc.Lt = &s
	case *validate.SInt64Rules_Lte:
		s := fmt.Sprintf("%d", lt.Lte)
		vc.Lte = &s
	}
}

// extractFixed32Bounds reads GreaterThan/LessThan oneof from Fixed32Rules.
func extractFixed32Bounds(r *validate.Fixed32Rules, vc *ValidateConstraints) {
	if r.Const != nil {
		s := fmt.Sprintf("%d", r.GetConst())
		vc.Const = &s
	}
	for _, v := range r.GetIn() {
		vc.In = append(vc.In, fmt.Sprintf("%d", v))
	}
	for _, v := range r.GetNotIn() {
		vc.NotIn = append(vc.NotIn, fmt.Sprintf("%d", v))
	}
	switch gt := r.GetGreaterThan().(type) {
	case *validate.Fixed32Rules_Gt:
		s := fmt.Sprintf("%d", gt.Gt)
		vc.Gt = &s
	case *validate.Fixed32Rules_Gte:
		s := fmt.Sprintf("%d", gt.Gte)
		vc.Gte = &s
	}
	switch lt := r.GetLessThan().(type) {
	case *validate.Fixed32Rules_Lt:
		s := fmt.Sprintf("%d", lt.Lt)
		vc.Lt = &s
	case *validate.Fixed32Rules_Lte:
		s := fmt.Sprintf("%d", lt.Lte)
		vc.Lte = &s
	}
}

// extractFixed64Bounds reads GreaterThan/LessThan oneof from Fixed64Rules.
func extractFixed64Bounds(r *validate.Fixed64Rules, vc *ValidateConstraints) {
	if r.Const != nil {
		s := fmt.Sprintf("%d", r.GetConst())
		vc.Const = &s
	}
	for _, v := range r.GetIn() {
		vc.In = append(vc.In, fmt.Sprintf("%d", v))
	}
	for _, v := range r.GetNotIn() {
		vc.NotIn = append(vc.NotIn, fmt.Sprintf("%d", v))
	}
	switch gt := r.GetGreaterThan().(type) {
	case *validate.Fixed64Rules_Gt:
		s := fmt.Sprintf("%d", gt.Gt)
		vc.Gt = &s
	case *validate.Fixed64Rules_Gte:
		s := fmt.Sprintf("%d", gt.Gte)
		vc.Gte = &s
	}
	switch lt := r.GetLessThan().(type) {
	case *validate.Fixed64Rules_Lt:
		s := fmt.Sprintf("%d", lt.Lt)
		vc.Lt = &s
	case *validate.Fixed64Rules_Lte:
		s := fmt.Sprintf("%d", lt.Lte)
		vc.Lte = &s
	}
}

// extractSFixed32Bounds reads GreaterThan/LessThan oneof from SFixed32Rules.
func extractSFixed32Bounds(r *validate.SFixed32Rules, vc *ValidateConstraints) {
	if r.Const != nil {
		s := fmt.Sprintf("%d", r.GetConst())
		vc.Const = &s
	}
	for _, v := range r.GetIn() {
		vc.In = append(vc.In, fmt.Sprintf("%d", v))
	}
	for _, v := range r.GetNotIn() {
		vc.NotIn = append(vc.NotIn, fmt.Sprintf("%d", v))
	}
	switch gt := r.GetGreaterThan().(type) {
	case *validate.SFixed32Rules_Gt:
		s := fmt.Sprintf("%d", gt.Gt)
		vc.Gt = &s
	case *validate.SFixed32Rules_Gte:
		s := fmt.Sprintf("%d", gt.Gte)
		vc.Gte = &s
	}
	switch lt := r.GetLessThan().(type) {
	case *validate.SFixed32Rules_Lt:
		s := fmt.Sprintf("%d", lt.Lt)
		vc.Lt = &s
	case *validate.SFixed32Rules_Lte:
		s := fmt.Sprintf("%d", lt.Lte)
		vc.Lte = &s
	}
}

// extractSFixed64Bounds reads GreaterThan/LessThan oneof from SFixed64Rules.
func extractSFixed64Bounds(r *validate.SFixed64Rules, vc *ValidateConstraints) {
	if r.Const != nil {
		s := fmt.Sprintf("%d", r.GetConst())
		vc.Const = &s
	}
	for _, v := range r.GetIn() {
		vc.In = append(vc.In, fmt.Sprintf("%d", v))
	}
	for _, v := range r.GetNotIn() {
		vc.NotIn = append(vc.NotIn, fmt.Sprintf("%d", v))
	}
	switch gt := r.GetGreaterThan().(type) {
	case *validate.SFixed64Rules_Gt:
		s := fmt.Sprintf("%d", gt.Gt)
		vc.Gt = &s
	case *validate.SFixed64Rules_Gte:
		s := fmt.Sprintf("%d", gt.Gte)
		vc.Gte = &s
	}
	switch lt := r.GetLessThan().(type) {
	case *validate.SFixed64Rules_Lt:
		s := fmt.Sprintf("%d", lt.Lt)
		vc.Lt = &s
	case *validate.SFixed64Rules_Lte:
		s := fmt.Sprintf("%d", lt.Lte)
		vc.Lte = &s
	}
}
