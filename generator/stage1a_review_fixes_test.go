package generator

import (
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"

	proto2typepb "github.com/protocgen/proto2type/proto/proto2type"
)

func reviewAppendMsgField(msg *descriptorpb.DescriptorProto, name string, num int32, typeName string, repeated bool, opts *descriptorpb.FieldOptions) *descriptorpb.FieldDescriptorProto {
	label := descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL
	if repeated {
		label = descriptorpb.FieldDescriptorProto_LABEL_REPEATED
	}
	f := &descriptorpb.FieldDescriptorProto{
		Name:     proto.String(name),
		Number:   proto.Int32(num),
		Label:    label.Enum(),
		Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
		TypeName: proto.String(typeName),
		JsonName: proto.String(name),
		Options:  opts,
	}
	msg.Field = append(msg.Field, f)
	return f
}

func TestStage1AReview_BoolDescWireAndEmptyIndexPart(t *testing.T) {
	// Wrong wire type (BytesType) for bool IndexPart.desc (tag 4) must decode safely to false,
	// while valid VarintType = 1 decodes to true.
	var goodPart, badWirePart []byte
	goodPart = protowire.AppendTag(goodPart, 1, protowire.BytesType)
	goodPart = protowire.AppendString(goodPart, "created_at")
	goodPart = protowire.AppendTag(goodPart, 4, protowire.VarintType)
	goodPart = protowire.AppendVarint(goodPart, 1)

	badWirePart = protowire.AppendTag(badWirePart, 1, protowire.BytesType)
	badWirePart = protowire.AppendString(badWirePart, "updated_at")
	badWirePart = protowire.AppendTag(badWirePart, 4, protowire.BytesType)
	badWirePart = protowire.AppendString(badWirePart, "DESC")

	var idxWire, tblWire []byte
	idxWire = protowire.AppendTag(idxWire, 1, protowire.BytesType)
	idxWire = protowire.AppendString(idxWire, "idx_wire_bool_desc")
	idxWire = protowire.AppendTag(idxWire, 7, protowire.BytesType)
	idxWire = protowire.AppendBytes(idxWire, goodPart)
	idxWire = protowire.AppendTag(idxWire, 7, protowire.BytesType)
	idxWire = protowire.AppendBytes(idxWire, badWirePart)

	tblWire = protowire.AppendTag(tblWire, 1, protowire.BytesType)
	tblWire = protowire.AppendString(tblWire, "wire_bool_desc_tbl")
	tblWire = protowire.AppendTag(tblWire, 20, protowire.BytesType)
	tblWire = protowire.AppendBytes(tblWire, idxWire)

	var msgOptsWire []byte
	msgOptsWire = protowire.AppendTag(msgOptsWire, 2, protowire.BytesType)
	msgOptsWire = protowire.AppendBytes(msgOptsWire, tblWire)
	mo := &descriptorpb.MessageOptions{}
	mo.ProtoReflect().SetUnknown(buildWireExtension(proto2typeExtensionNumber, msgOptsWire))
	fdp := stage1aSingleMsgFDP("wire_bool_desc.proto", "review.v1", "example.com/review/v1;reviewv1", &descriptorpb.DescriptorProto{
		Name:    proto.String("WireBoolDesc"),
		Options: mo,
		Field: []*descriptorpb.FieldDescriptorProto{
			stage1aScalarField("id", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, stage1aColOpts(nil, &proto2typepb.FieldOptions{PrimaryKey: true})),
			stage1aScalarField("created_at", 2, descriptorpb.FieldDescriptorProto_TYPE_INT64, false, nil),
			stage1aScalarField("updated_at", 3, descriptorpb.FieldDescriptorProto_TYPE_INT64, false, nil),
		},
	})
	gen := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{fdp}}, []string{"wire_bool_desc.proto"})
	meta := extractDorisTableMeta(gen.Files[0].Messages[0])
	if meta == nil || len(meta.Indexes) != 1 || len(meta.Indexes[0].Parts) != 2 {
		t.Fatalf("unexpected decoded wire IndexDef: %+v", meta)
	}
	if !meta.Indexes[0].Parts[0].Desc || meta.Indexes[0].Parts[1].Desc {
		t.Fatalf("expected parts desc=[true, false], got %+v", meta.Indexes[0].Parts)
	}

	// Empty IndexPart entries (typed & wire) must NOT be silently dropped; downstream validation must reject them.
	emptyPartMsg := &descriptorpb.DescriptorProto{
		Name: proto.String("EmptyPartTbl"),
		Options: stage1aMsgOpts(&proto2typepb.TableOptions{
			TableName: "empty_part_tbl",
			Indexes: []*proto2typepb.IndexDef{
				{
					Name:  "idx_empty_part",
					Parts: []*proto2typepb.IndexDef_IndexPart{{}},
				},
			},
		}),
		Field: []*descriptorpb.FieldDescriptorProto{
			stage1aScalarField("id", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, stage1aColOpts(nil, &proto2typepb.FieldOptions{PrimaryKey: true})),
		},
	}
	emptyFDP := stage1aSingleMsgFDP("empty_part.proto", "review.v1", "example.com/review/v1;reviewv1", emptyPartMsg)
	emptyGen := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{emptyFDP}}, []string{"empty_part.proto"})
	runner := NewRunner()
	if err := runner.Generate(emptyGen, &Options{Lang: "go", Backend: "postgres", PostgresMode: "ddl"}); err == nil {
		t.Errorf("expected relational postgres generation error for empty IndexPart, got nil")
	}
	lakeEmptyMsg := proto.Clone(emptyPartMsg).(*descriptorpb.DescriptorProto)
	lakeEmptyMsg.Options = stage1aMsgOpts(&proto2typepb.TableOptions{
		TableName:     "empty_part_lake",
		DatabaseScope: "TENANT",
		SyncTier:      "WHOLE_TABLE",
		Indexes: []*proto2typepb.IndexDef{
			{
				Name:  "idx_empty_part_lake",
				Parts: []*proto2typepb.IndexDef_IndexPart{{}},
			},
		},
	})
	lakeFDP := stage1aSingleMsgFDP("empty_part_lake.proto", "review.v1", "example.com/review/v1;reviewv1", lakeEmptyMsg)
	lakeGen := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{lakeFDP}}, []string{"empty_part_lake.proto"})
	if err := runner.Generate(lakeGen, &Options{Lang: "go", Backend: "postgres", PostgresMode: "hcl"}); err == nil {
		t.Errorf("expected lakehouse postgres generation error for empty IndexPart, got nil")
	}
}

func TestStage1AReview_SingletonValidationAndSyntheticPK(t *testing.T) {
	runner := NewRunner()

	// 1. Lakehouse and relational synthetic singleton (no explicit singleton column in proto).
	synthRelFDP := stage1aSingleMsgFDP("synth_rel.proto", "review.v1", "example.com/review/v1;reviewv1", &descriptorpb.DescriptorProto{
		Name: proto.String("RelSingletonCfg"),
		Options: stage1aMsgOpts(&proto2typepb.TableOptions{
			TableName:   "rel_singleton_cfg",
			IsSingleton: true,
		}),
		Field: []*descriptorpb.FieldDescriptorProto{
			stage1aScalarField("setting_val", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, nil),
		},
	})
	relGen := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{synthRelFDP}}, []string{"synth_rel.proto"})
	if err := runner.Generate(relGen, &Options{Lang: "go", Backend: "postgres", PostgresMode: "ddl+hcl"}); err != nil {
		t.Fatalf("relational synthetic singleton failed: %v", err)
	}
	relFiles := stage1aGeneratedFiles(t, relGen)
	if sql := stage1aFindBySuffix(t, relFiles, "_postgres.sql"); !strings.Contains(sql, `"singleton" BOOLEAN NOT NULL DEFAULT TRUE`) || !strings.Contains(sql, `PRIMARY KEY ("singleton")`) {
		t.Errorf("unexpected relational synthetic singleton SQL:\n%s", sql)
	}

	synthLakeFDP := stage1aSingleMsgFDP("synth_lake.proto", "review.v1", "example.com/review/v1;reviewv1", &descriptorpb.DescriptorProto{
		Name: proto.String("LakeSingletonCfg"),
		Options: stage1aMsgOpts(&proto2typepb.TableOptions{
			TableName:     "lake_singleton_cfg",
			DatabaseScope: "TENANT",
			SyncTier:      "WHOLE_TABLE",
			IsSingleton:   true,
		}),
		Field: []*descriptorpb.FieldDescriptorProto{
			stage1aScalarField("setting_val", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, nil),
		},
	})
	lakeGen := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{synthLakeFDP}}, []string{"synth_lake.proto"})
	if err := runner.Generate(lakeGen, &Options{Lang: "go", Backend: "postgres", PostgresMode: "ddl+hcl"}); err != nil {
		t.Fatalf("lakehouse synthetic singleton failed: %v", err)
	}
	lakeFiles := stage1aGeneratedFiles(t, lakeGen)
	if sql := stage1aFindBySuffix(t, lakeFiles, "_postgres.sql"); !strings.Contains(sql, `singleton BOOLEAN NOT NULL DEFAULT true`) || !strings.Contains(sql, `PRIMARY KEY (singleton)`) {
		t.Errorf("unexpected lakehouse synthetic singleton SQL:\n%s", sql)
	}

	// 2. Existing nullable, non-PK singleton column must be forced Nullable=false and IsPrimaryKey=true.
	existingNullableOpt := stage1aColOpts(&proto2typepb.ColumnOptions{Nullable: proto2typepb.OptionalBool_OPTIONAL_BOOL_TRUE}, nil)
	for _, tc := range []struct {
		name string
		lake bool
	}{
		{name: "relational", lake: false},
		{name: "lakehouse", lake: true},
	} {
		t.Run("existing_singleton_col/"+tc.name, func(t *testing.T) {
			tblOpts := &proto2typepb.TableOptions{
				TableName:   "existing_singleton_" + tc.name,
				IsSingleton: true,
			}
			if tc.lake {
				tblOpts.DatabaseScope = "TENANT"
				tblOpts.SyncTier = "WHOLE_TABLE"
			}
			msg := &descriptorpb.DescriptorProto{
				Name:    proto.String("ExistingSingleton"),
				Options: stage1aMsgOpts(tblOpts),
				Field: []*descriptorpb.FieldDescriptorProto{
					stage1aScalarField("singleton", 1, descriptorpb.FieldDescriptorProto_TYPE_BOOL, false, existingNullableOpt),
					stage1aScalarField("label", 2, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, nil),
				},
			}
			fdp := stage1aSingleMsgFDP("existing_"+tc.name+".proto", "review.v1", "example.com/review/v1;reviewv1", msg)
			gen := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{fdp}}, []string{"existing_" + tc.name + ".proto"})
			if err := runner.Generate(gen, &Options{Lang: "go", Backend: "postgres", PostgresMode: "hcl"}); err != nil {
				t.Fatalf("Generate failed: %v", err)
			}
			files := stage1aGeneratedFiles(t, gen)
			hclOut := stage1aFindBySuffix(t, files, "_postgres.hcl")
			parsed, err := ParsePostgresAtlasHCL(hclOut)
			if err != nil {
				t.Fatalf("ParsePostgresAtlasHCL failed: %v\n%s", err, hclOut)
			}
			if len(parsed) != 1 || len(parsed[0].PrimaryKeyColumns) != 1 || parsed[0].PrimaryKeyColumns[0] != "singleton" {
				t.Fatalf("expected PrimaryKeyColumns=[singleton], got %+v", parsed[0].PrimaryKeyColumns)
			}
			if parsed[0].Columns[0].Nullable {
				t.Fatalf("expected singleton column Nullable=false, got true")
			}
		})
	}

	// 3. singleton_check_name / singleton_check_expr without is_singleton=true must fail in both paths.
	for _, fieldSet := range []struct {
		name string
		opts *proto2typepb.TableOptions
	}{
		{name: "rel_check_name", opts: &proto2typepb.TableOptions{TableName: "bad_s1", SingletonCheckName: proto.String("chk_x")}},
		{name: "rel_check_expr", opts: &proto2typepb.TableOptions{TableName: "bad_s2", SingletonCheckExpr: proto.String("(id = 1)")}},
		{name: "lake_check_name", opts: &proto2typepb.TableOptions{TableName: "bad_s3", DatabaseScope: "TENANT", SingletonCheckName: proto.String("chk_y")}},
		{name: "lake_check_expr", opts: &proto2typepb.TableOptions{TableName: "bad_s4", DatabaseScope: "TENANT", SingletonCheckExpr: proto.String("(id = 1)")}},
	} {
		t.Run("reject_non_singleton/"+fieldSet.name, func(t *testing.T) {
			msg := &descriptorpb.DescriptorProto{
				Name:    proto.String("BadSingletonOpts"),
				Options: stage1aMsgOpts(fieldSet.opts),
				Field: []*descriptorpb.FieldDescriptorProto{
					stage1aScalarField("id", 1, descriptorpb.FieldDescriptorProto_TYPE_INT32, false, stage1aColOpts(nil, &proto2typepb.FieldOptions{PrimaryKey: true})),
				},
			}
			fdp := stage1aSingleMsgFDP(fieldSet.name+".proto", "review.v1", "example.com/review/v1;reviewv1", msg)
			gen := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{fdp}}, []string{fieldSet.name + ".proto"})
			if err := runner.Generate(gen, &Options{Lang: "go", Backend: "postgres", PostgresMode: "ddl"}); err == nil {
				t.Errorf("expected error when %s set with is_singleton=false", fieldSet.name)
			}
		})
	}
}

func TestStage1AReview_ConsolidatedGoCollisionsAndCrossFileMsgByName(t *testing.T) {
	runner := NewRunner()

	// 1. Directory-qualified DorisTables registry names when basenames collide in consolidated doris Go.
	mkDorisFDP := func(fileName, protoPkg, goPkg, msgName, tblName string) *descriptorpb.FileDescriptorProto {
		return stage1aSingleMsgFDP(fileName, protoPkg, goPkg, &descriptorpb.DescriptorProto{
			Name: proto.String(msgName),
			Options: stage1aMsgOpts(&proto2typepb.TableOptions{
				TableName:        tblName,
				DatabaseScope:    "TENANT",
				SyncTier:         "WHOLE_TABLE",
				UniqueKeyColumns: []string{"tenant_id", "id"},
			}),
			Field: []*descriptorpb.FieldDescriptorProto{
				stage1aScalarField("tenant_id", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, stage1aColOpts(&proto2typepb.ColumnOptions{DorisType: "VARCHAR(64)"}, nil)),
				stage1aScalarField("id", 2, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, stage1aColOpts(&proto2typepb.ColumnOptions{DorisType: "VARCHAR(64)"}, nil)),
			},
		})
	}
	fdsQualified := &descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{
			mkDorisFDP("sub1/events.proto", "sub1.v1", "example.com/shared/v1;sharedv1", "SubOneEvent", "sub1_events"),
			mkDorisFDP("sub2/events.proto", "sub2.v1", "example.com/shared/v1;sharedv1", "SubTwoEvent", "sub2_events"),
		},
	}
	genQual := newPlugin(t, fdsQualified, []string{"sub1/events.proto", "sub2/events.proto"})
	if err := runner.Generate(genQual, &Options{Lang: "go", Backend: "doris", DorisMode: "go", OutputFile: "consolidated_doris.go"}); err != nil {
		t.Fatalf("consolidated doris Go generation failed: %v", err)
	}
	qualOut := stage1aGeneratedFiles(t, genQual)["consolidated_doris.go"]
	for _, want := range []string{"var Sub1EventsDorisTables = []DorisTableDescriptor{", "var Sub2EventsDorisTables = []DorisTableDescriptor{"} {
		if !strings.Contains(qualOut, want) {
			t.Errorf("missing qualified registry %q in:\n%s", want, qualOut)
		}
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "consolidated_doris.go", qualOut, parser.AllErrors); err != nil {
		t.Fatalf("qualified consolidated doris Go does not parse: %v", err)
	}

	// 2. Fail fast on Go symbol collisions across consolidated files (including opts.GoPackage override).
	fdsDorisCollide := &descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{
			mkDorisFDP("a/one.proto", "a.v1", "example.com/a/v1;av1", "SameName", "table_one"),
			mkDorisFDP("b/two.proto", "b.v1", "example.com/b/v1;bv1", "SameName", "table_two"),
		},
	}
	genDorisCol := newPlugin(t, fdsDorisCollide, []string{"a/one.proto", "b/two.proto"})
	if err := runner.Generate(genDorisCol, &Options{
		Lang:       "go",
		Backend:    "doris",
		DorisMode:  "go",
		GoPackage:  "example.com/merged;merged",
		OutputFile: "merged_doris.go",
	}); err == nil {
		t.Errorf("expected fail-fast collision error in consolidated doris go with GoPackage override")
	}

	mkRelFDP := func(fileName, protoPkg, goPkg, msgName, tblName string) *descriptorpb.FileDescriptorProto {
		return stage1aSingleMsgFDP(fileName, protoPkg, goPkg, &descriptorpb.DescriptorProto{
			Name:    proto.String(msgName),
			Options: stage1aMsgOpts(&proto2typepb.TableOptions{TableName: tblName}),
			Field: []*descriptorpb.FieldDescriptorProto{
				stage1aScalarField("id", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, stage1aColOpts(nil, &proto2typepb.FieldOptions{PrimaryKey: true})),
			},
		})
	}
	fdsPgCollide := &descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{
			mkRelFDP("a/one.proto", "a.v1", "example.com/a/v1;av1", "SharedEntity", "shared_a"),
			mkRelFDP("b/two.proto", "b.v1", "example.com/b/v1;bv1", "SharedEntity", "shared_b"),
		},
	}
	genPgGoCol := newPlugin(t, fdsPgCollide, []string{"a/one.proto", "b/two.proto"})
	if err := runner.Generate(genPgGoCol, &Options{
		Lang:         "go",
		Backend:      "postgres",
		PostgresMode: "go",
		GoPackage:    "example.com/merged;merged",
		OutputFile:   "merged_pg.go",
	}); err == nil {
		t.Errorf("expected fail-fast collision error in consolidated postgres go")
	}
	genSearchCol := newPlugin(t, fdsPgCollide, []string{"a/one.proto", "b/two.proto"})
	if err := runner.Generate(genSearchCol, &Options{
		Lang:         "go",
		Backend:      "postgres",
		PostgresMode: "search_index",
		GoPackage:    "example.com/merged;merged",
		OutputFile:   "merged_spidx.go",
	}); err == nil {
		t.Errorf("expected fail-fast collision error in consolidated search_index go")
	}

	// 3. Cross-package same-short-name Address inline_nested keying via FullName in go_postgres + lakehouse embedded exclusion.
	fdpAddrA := stage1aSingleMsgFDP("a/addr.proto", "a.v1", "example.com/shared;shared", &descriptorpb.DescriptorProto{
		Name: proto.String("Address"),
		Field: []*descriptorpb.FieldDescriptorProto{
			stage1aScalarField("street_a", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, nil),
		},
	})
	fdpAddrB := stage1aSingleMsgFDP("b/addr.proto", "b.v1", "example.com/shared;shared", &descriptorpb.DescriptorProto{
		Name: proto.String("Address"),
		Field: []*descriptorpb.FieldDescriptorProto{
			stage1aScalarField("street_b", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, nil),
		},
	})
	useA := &descriptorpb.DescriptorProto{
		Name:    proto.String("UserA"),
		Options: stage1aMsgOpts(&proto2typepb.TableOptions{TableName: "users_a"}),
		Field: []*descriptorpb.FieldDescriptorProto{
			stage1aScalarField("id", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, stage1aColOpts(nil, &proto2typepb.FieldOptions{PrimaryKey: true})),
		},
	}
	reviewAppendMsgField(useA, "addr", 2, ".a.v1.Address", false, stage1aColOpts(&proto2typepb.ColumnOptions{InlineNested: true}, nil))
	useB := &descriptorpb.DescriptorProto{
		Name:    proto.String("UserB"),
		Options: stage1aMsgOpts(&proto2typepb.TableOptions{TableName: "users_b"}),
		Field: []*descriptorpb.FieldDescriptorProto{
			stage1aScalarField("id", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, stage1aColOpts(nil, &proto2typepb.FieldOptions{PrimaryKey: true})),
		},
	}
	reviewAppendMsgField(useB, "addr", 2, ".b.v1.Address", false, stage1aColOpts(&proto2typepb.ColumnOptions{InlineNested: true}, nil))
	fdpUsers := &descriptorpb.FileDescriptorProto{
		Syntax:      proto.String("proto3"),
		Name:        proto.String("users.proto"),
		Package:     proto.String("app.v1"),
		Dependency:  []string{"a/addr.proto", "b/addr.proto"},
		Options:     &descriptorpb.FileOptions{GoPackage: proto.String("example.com/shared;shared")},
		MessageType: []*descriptorpb.DescriptorProto{useA, useB},
	}
	genCross := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{fdpAddrA, fdpAddrB, fdpUsers}}, []string{"users.proto"})
	dfA, err := BuildDomainFile(genCross.Files[0], &Options{})
	if err != nil {
		t.Fatalf("BuildDomainFile A: %v", err)
	}
	dfB, err := BuildDomainFile(genCross.Files[1], &Options{})
	if err != nil {
		t.Fatalf("BuildDomainFile B: %v", err)
	}
	dfU, err := BuildDomainFile(genCross.Files[2], &Options{})
	if err != nil {
		t.Fatalf("BuildDomainFile Users: %v", err)
	}
	msgByName := make(map[string]*DomainMessage)
	for _, df := range []*DomainFile{dfA, dfB, dfU} {
		for k, v := range indexDomainMessagesByFullName(df.Messages) {
			msgByName[k] = v
		}
	}
	flatA, okA := cloneDomainMessageWithFlattenedInline(dfU.Messages[0], msgByName)
	flatB, okB := cloneDomainMessageWithFlattenedInline(dfU.Messages[1], msgByName)
	if !okA || !okB {
		t.Fatalf("expected inline flattening for both a.v1.Address and b.v1.Address")
	}
	if flatA.Fields[1].Name != "street_a" || flatB.Fields[1].Name != "street_b" {
		t.Fatalf("FullName collision flattened wrong Address fields: A=%q B=%q", flatA.Fields[1].Name, flatB.Fields[1].Name)
	}

	// Mark lakehouse embedded messages before collecting relational tables.
	lakeEmbedded := make(map[string]bool)
	markDomainMessagesEmbedded([]*DomainFile{dfU}, lakeEmbedded)
	if !lakeEmbedded["a.v1.Address"] || !lakeEmbedded["b.v1.Address"] {
		t.Fatalf("expected markDomainMessagesEmbedded to record FullNames, got %+v", lakeEmbedded)
	}
	if nonEmbedded, err := collectPostgresTableSchemasMultiWithEmbedded([]*DomainFile{dfA}, nil, &Options{}); err != nil || len(nonEmbedded) != 1 {
		t.Fatalf("expected 1 standalone relational table for Address without lakehouse embedding, got (%d, %v)", len(nonEmbedded), err)
	}
	relSchemas, err := collectPostgresTableSchemasMultiWithEmbedded([]*DomainFile{dfA, dfB}, lakeEmbedded, &Options{})
	if err != nil {
		t.Fatalf("collectPostgresTableSchemasMultiWithEmbedded failed: %v", err)
	}
	if len(relSchemas) != 0 {
		t.Fatalf("expected lakehouse-embedded messages to be excluded from relational tables, got %d", len(relSchemas))
	}

	// 4. irNeedsTime detects Timestamp/Duration in Oneofs and MapValue.
	oneofTimeMsgs := []*DomainMessage{{
		Name:   "WithOneofTime",
		Oneofs: []*DomainOneof{{Name: "Choice", Variants: []*OneofVariant{{Name: "ts", Kind: FieldKindTimestamp}}}},
	}}
	if !irNeedsTime(oneofTimeMsgs) {
		t.Errorf("expected irNeedsTime=true for oneof Timestamp variant")
	}
	mapDurMsgs := []*DomainMessage{{
		Name:   "WithMapDur",
		Fields: []*DomainField{{Name: "durations", IsMap: true, MapValue: &MapTypeInfo{Kind: FieldKindDuration}}},
	}}
	if !irNeedsTime(mapDurMsgs) {
		t.Errorf("expected irNeedsTime=true for map Duration value")
	}
}

func TestStage1AReview_SecurityDescriptorValidationAndHCLQuoting(t *testing.T) {
	baseDesc := func() PostgresTableDescriptor {
		return PostgresTableDescriptor{
			TableName:     "sec_tbl",
			DatabaseScope: "PUBLIC",
			Columns: []PostgresColumnDescriptor{
				{ColumnName: "id", SQLType: "UUID", HCLType: "uuid", IsPrimaryKey: true},
				{ColumnName: "title", SQLType: "TEXT", HCLType: "text"},
			},
			PrimaryKeyColumns: []string{"id"},
		}
	}
	if !validatePostgresDescriptor(baseDesc()) {
		t.Fatalf("expected valid base PostgresTableDescriptor")
	}

	invalidDescs := []struct {
		name string
		mod  func(*PostgresTableDescriptor)
	}{
		{"unknown_inverted_col", func(d *PostgresTableDescriptor) { d.InvertedIndexColumns = []string{"missing"} }},
		{"bad_ngram_col", func(d *PostgresTableDescriptor) { d.NgramBfColumns = []string{"bad;col"} }},
		{"idx_unknown_col", func(d *PostgresTableDescriptor) {
			d.Indexes = []PostgresIndexDescriptor{{Name: "idx_x", Columns: []string{"missing"}}}
		}},
		{"idx_invalid_using", func(d *PostgresTableDescriptor) {
			d.Indexes = []PostgresIndexDescriptor{{Name: "idx_x", Columns: []string{"title"}, Using: "BTREE; DROP TABLE sec_tbl"}}
		}},
		{"idx_invalid_ops", func(d *PostgresTableDescriptor) {
			d.Indexes = []PostgresIndexDescriptor{{Name: "idx_x", Columns: []string{"title"}, Using: "GIN", Ops: "bad;ops"}}
		}},
		{"idx_part_unknown_col", func(d *PostgresTableDescriptor) {
			d.Indexes = []PostgresIndexDescriptor{{Name: "idx_x", Parts: []PostgresIndexPartDescriptor{{Column: "missing"}}}}
		}},
		{"fk_unknown_col", func(d *PostgresTableDescriptor) {
			d.ForeignKeys = []PostgresForeignKeyDescriptor{{Name: "fk_x", Columns: []string{"missing"}, RefTable: "other", RefColumns: []string{"id"}}}
		}},
		{"fk_bad_ref_col", func(d *PostgresTableDescriptor) {
			d.ForeignKeys = []PostgresForeignKeyDescriptor{{Name: "fk_x", Columns: []string{"id"}, RefTable: "other", RefColumns: []string{strings.Repeat("r", 64)}}}
		}},
		{"fk_bad_on_delete", func(d *PostgresTableDescriptor) {
			d.ForeignKeys = []PostgresForeignKeyDescriptor{{Name: "fk_x", Columns: []string{"id"}, RefTable: "other", RefColumns: []string{"id"}, OnDelete: "DROP TABLE"}}
		}},
		{"fk_bad_on_update", func(d *PostgresTableDescriptor) {
			d.ForeignKeys = []PostgresForeignKeyDescriptor{{Name: "fk_x", Columns: []string{"id"}, RefTable: "other", RefColumns: []string{"id"}, OnUpdate: "CASCADE; --"}}
		}},
		{"chk_name_too_long", func(d *PostgresTableDescriptor) {
			d.Checks = []PostgresCheckDescriptor{{Name: strings.Repeat("c", 64), Expr: "(title <> '')"}}
		}},
		{"chk_backslash_breakout", func(d *PostgresTableDescriptor) {
			d.Checks = []PostgresCheckDescriptor{{Name: "chk_esc", Expr: `title = 'x\'' OR 1=1`}}
		}},
		{"chk_dollar_quote_empty", func(d *PostgresTableDescriptor) {
			d.Checks = []PostgresCheckDescriptor{{Name: "chk_dq", Expr: `title = $$a' OR '1'='1$$`}}
		}},
		{"chk_dollar_quote_tagged", func(d *PostgresTableDescriptor) {
			d.Checks = []PostgresCheckDescriptor{{Name: "chk_dq2", Expr: `title = $tag$evil$tag$`}}
		}},
	}
	for _, tc := range invalidDescs {
		t.Run("validate_desc/"+tc.name, func(t *testing.T) {
			d := baseDesc()
			tc.mod(&d)
			if validatePostgresDescriptor(d) {
				t.Errorf("expected validatePostgresDescriptor to reject %s", tc.name)
			}
		})
	}

	// Reject backslash and dollar-quoting directly in isValidPostgresCheckExpr & hasOuterBalancedParens.
	for _, expr := range []string{
		`E'\''`,
		`title = 'a\' OR 1=1'`,
		`$$hello$$`,
		`$tag$hello$tag$`,
	} {
		if isValidPostgresCheckExpr(expr) {
			t.Errorf("isValidPostgresCheckExpr unexpectedly accepted %q", expr)
		}
		if hasOuterBalancedParens("(" + expr + ")") {
			t.Errorf("hasOuterBalancedParens unexpectedly accepted %q", expr)
		}
	}

	// Reject unknown sub-block, invalid type/using, invalid ops in ParsePostgresAtlasHCL.
	for _, hcl := range []struct {
		name string
		body string
	}{
		{
			name: "unknown_index_subblock",
			body: `table "t1" {
  column "id" { null = false type = uuid }
  index "idx_t1" {
    unknown_blk { foo = "bar" }
    columns = [column.id]
  }
}`,
		},
		{
			name: "invalid_index_using",
			body: `table "t1" {
  column "id" { null = false type = text }
  index "idx_t1" {
    type    = "EXEC"
    columns = [column.id]
  }
}`,
		},
		{
			name: "invalid_index_ops",
			body: `table "t1" {
  column "id" { null = false type = text }
  index "idx_t1" {
    type    = GIN
    ops     = "bad;ops"
    columns = [column.id]
  }
}`,
		},
	} {
		if _, err := ParsePostgresAtlasHCL(hcl.body); err == nil {
			t.Errorf("ParsePostgresAtlasHCL(%s): expected error, got nil", hcl.name)
		}
	}

	// quoteHCLString escapes ${ -> $${ and %{ -> %%{ and unescapes cleanly on parse while preserving ${TENANT_*}.
	interpDesc := PostgresTableDescriptor{
		TableName:     "interp_tbl",
		DatabaseScope: "PUBLIC",
		Columns: []PostgresColumnDescriptor{
			{ColumnName: "id", SQLType: "UUID", HCLType: "uuid", IsPrimaryKey: true},
			{ColumnName: "tags", SQLType: "TEXT[]", HCLType: "text[]", HasDefaultValue: true, DefaultValue: "{${ok},%{dir}}"},
			{ColumnName: "expr_col", SQLType: "TEXT", HCLType: "text", HasDefaultValue: true, DefaultValue: "already"},
		},
		PrimaryKeyColumns: []string{"id"},
		Indexes: []PostgresIndexDescriptor{
			{
				Name:  "idx_interp_expr",
				Parts: []PostgresIndexPartDescriptor{{Expr: "coalesce(expr_col, '${fallback}')"}},
				Where: "(expr_col <> '%{directive}')",
			},
		},
		Checks: []PostgresCheckDescriptor{
			{Name: "chk_interp", Expr: "(expr_col <> '${blocked}')"},
		},
	}
	hclDoc := interpDesc.BuildPostgresAtlasHCL("")
	for _, want := range []string{`$${blocked}`, `%%{directive}`, `$${fallback}`, `$${ok}`, `%%{dir}`} {
		if !strings.Contains(hclDoc, want) {
			t.Errorf("expected escaped HCL interpolation %q in:\n%s", want, hclDoc)
		}
	}
	parsedInterp, err := ParsePostgresAtlasHCL(hclDoc)
	if err != nil {
		t.Fatalf("ParsePostgresAtlasHCL failed: %v\n%s", err, hclDoc)
	}
	if parsedInterp[0].Checks[0].Expr != "(expr_col <> '${blocked}')" {
		t.Errorf("check expr round-trip = %q", parsedInterp[0].Checks[0].Expr)
	}
	if parsedInterp[0].Indexes[0].Where != "(expr_col <> '%{directive}')" {
		t.Errorf("index where round-trip = %q", parsedInterp[0].Indexes[0].Where)
	}
	if parsedInterp[0].Indexes[0].Parts[0].Expr != "coalesce(expr_col, '${fallback}')" {
		t.Errorf("index part expr round-trip = %q", parsedInterp[0].Indexes[0].Parts[0].Expr)
	}

	// DefaultValue starting and ending with ' must NEVER skip quoting (injection defense).
	quoteCol := PostgresColumnDescriptor{
		ColumnName:      "expr_col",
		SQLType:         "TEXT",
		HCLType:         "text",
		HasDefaultValue: true,
		DefaultValue:    "'hello' OR '1'='1'",
	}
	if got := formatPostgresDefaultLiteral("TEXT", quoteCol.DefaultValue); got != `'''hello'' OR ''1''=''1'''` {
		t.Errorf("formatPostgresDefaultLiteral quote bypass failed: got %s", got)
	}
	if got := formatPostgresHCLDefaultValue(quoteCol); got != `"'hello' OR '1'='1'"` {
		t.Errorf("formatPostgresHCLDefaultValue quote bypass failed: got %s", got)
	}
	if got := formatPostgresSQLDefaultValue(quoteCol); got != `'''hello'' OR ''1''=''1'''` {
		t.Errorf("formatPostgresSQLDefaultValue quote bypass failed: got %s", got)
	}

	// Doris HCL preserves ${TENANT_SLUG} & ${TENANT_BUCKETS} while escaping ${foo} / %{bar} in comments/defaults.
	dorisTbl := DorisTableDescriptor{
		TableName:        "doris_interp_tbl",
		DatabaseScope:    "TENANT",
		SyncTier:         "WHOLE_TABLE",
		UniqueKeyColumns: []string{"tenant_id", "id"},
		Columns: []DorisColumnDescriptor{
			{ColumnName: "tenant_id", DorisType: "VARCHAR(64)", IsUniqueKey: true},
			{ColumnName: "id", DorisType: "VARCHAR(64)", IsUniqueKey: true},
			{ColumnName: "note", DorisType: "VARCHAR(128)", DefaultValue: "a${x}", Comment: "c%{y}"},
		},
	}
	dorisHCL := dorisTbl.BuildDorisAtlasHCL("", 0)
	for _, want := range []string{`buckets = "${TENANT_BUCKETS}"`, `schema.tenant_${TENANT_SLUG}`, `"a$${x}"`, `"c%%{y}"`} {
		if !strings.Contains(dorisHCL, want) {
			t.Errorf("Doris HCL missing %q in:\n%s", want, dorisHCL)
		}
	}
	parsedDoris, err := ParseDorisAtlasHCL(dorisHCL)
	if err != nil {
		t.Fatalf("ParseDorisAtlasHCL failed: %v\n%s", err, dorisHCL)
	}
	if parsedDoris[0].Columns[2].DefaultValue != "a${x}" {
		t.Errorf("expected unescaped doris default 'a${x}', got %q", parsedDoris[0].Columns[2].DefaultValue)
	}

	// Relational explicit check name >63 bytes and bad FK actions must error.
	longChkMsg := stage1aSingleMsgFDP("rel_long_chk.proto", "review.v1", "example.com/review/v1;reviewv1", &descriptorpb.DescriptorProto{
		Name: proto.String("LongChkRel"),
		Options: stage1aMsgOpts(&proto2typepb.TableOptions{
			TableName: "long_chk_rel",
			Checks:    []*proto2typepb.CheckDef{{Name: strings.Repeat("c", 64), Expr: "(status >= 0)"}},
		}),
		Field: []*descriptorpb.FieldDescriptorProto{
			stage1aScalarField("id", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, stage1aColOpts(nil, &proto2typepb.FieldOptions{PrimaryKey: true})),
			stage1aScalarField("status", 2, descriptorpb.FieldDescriptorProto_TYPE_INT32, false, nil),
		},
	})
	runner := NewRunner()
	if err := runner.Generate(newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{longChkMsg}}, []string{"rel_long_chk.proto"}), &Options{Lang: "go", Backend: "postgres", PostgresMode: "ddl"}); err == nil {
		t.Errorf("expected error for explicit relational check name > 63 bytes")
	}
	badFKMsg := stage1aSingleMsgFDP("rel_bad_fk.proto", "review.v1", "example.com/review/v1;reviewv1", &descriptorpb.DescriptorProto{
		Name: proto.String("BadFKRel"),
		Options: stage1aMsgOpts(&proto2typepb.TableOptions{
			TableName:   "bad_fk_rel",
			ForeignKeys: []*proto2typepb.ForeignKeyDef{{Name: "fk_bad", Columns: []string{"parent_id"}, RefTable: "parents", RefColumns: []string{"id"}, OnDelete: "DROP CASCADE"}},
		}),
		Field: []*descriptorpb.FieldDescriptorProto{
			stage1aScalarField("id", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, stage1aColOpts(nil, &proto2typepb.FieldOptions{PrimaryKey: true})),
			stage1aScalarField("parent_id", 2, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, nil),
		},
	})
	if err := runner.Generate(newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{badFKMsg}}, []string{"rel_bad_fk.proto"}), &Options{Lang: "go", Backend: "postgres", PostgresMode: "ddl"}); err == nil {
		t.Errorf("expected error for relational ForeignKeyDef invalid on_delete action")
	}
}

func TestStage1AReview_CorrectnessH1ToH10AndMedium(t *testing.T) {
	runner := NewRunner()

	// 1. willEmitDomain + ValidateOutputFileOptions allow schema-only output_file with Domain:true CLI default,
	//    while still checking targetCount > 1 across all languages.
	for _, tc := range []struct {
		name    string
		opts    *Options
		wantErr bool
	}{
		{"pg_hcl_default_domain_true", &Options{Lang: "go", Backend: "postgres", PostgresMode: "hcl", Domain: true, OutputFile: "pg.hcl"}, false},
		{"doris_hcl_default_domain_true", &Options{Lang: "go", Backend: "doris", DorisMode: "hcl", Domain: true, OutputFile: "doris.hcl"}, false},
		{"starrocks_ddl_default_domain_true", &Options{Lang: "go", Backend: "doris", DorisMode: "starrocks_ddl", Domain: true, OutputFile: "sr.sql"}, false},
		{"rust_postgres_multi_target", &Options{Lang: "rust", Backend: "postgres", PostgresMode: "ddl+hcl", Domain: false, OutputFile: "out.sql"}, true},
	} {
		err := ValidateOutputFileOptions(tc.opts, 1)
		if tc.wantErr && err == nil {
			t.Errorf("%s: expected error, got nil", tc.name)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("%s: unexpected error: %v", tc.name, err)
		}
	}

	// Also verify unused import removal if protogen not needed (mark _ = (*protogen.Plugin)(nil)) — actually Runner.Generate
	// end-to-end with Domain:true CLI default and OutputFile succeeds on schema-only postgres_mode=hcl without blank-line artifacts.
	hclDefaultGen := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{
		stage1aSingleMsgFDP("hcl_def.proto", "review.v1", "example.com/review/v1;reviewv1", &descriptorpb.DescriptorProto{
			Name:    proto.String("HclDefaultTbl"),
			Options: stage1aMsgOpts(&proto2typepb.TableOptions{TableName: "hcl_default_tbl"}),
			Field: []*descriptorpb.FieldDescriptorProto{
				stage1aScalarField("id", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, stage1aColOpts(nil, &proto2typepb.FieldOptions{PrimaryKey: true})),
			},
		}),
	}}, []string{"hcl_def.proto"})
	if err := runner.Generate(hclDefaultGen, &Options{Lang: "go", Backend: "postgres", PostgresMode: "hcl", Domain: true, OutputFile: "single.hcl"}); err != nil {
		t.Fatalf("Runner.Generate with Domain:true and PostgresMode:hcl failed: %v", err)
	}
	singleHCL := stage1aGeneratedFiles(t, hclDefaultGen)["single.hcl"]
	if strings.Contains(singleHCL, "\n\n\n") || !strings.Contains(singleHCL, "\n\n# HclDefaultTbl (hcl_default_tbl)\ntable \"hcl_default_tbl\"") {
		t.Errorf("unexpected blank-line formatting in PostgreSQL HCL:\n%q", singleHCL)
	}

	// Verify ValidateOutputFile edge cases.
	for _, validPath := range []string{"schema..v2.hcl", "out.proto", "dir/sub/file.go"} {
		if got, err := ValidateOutputFile(validPath); err != nil || got != validPath {
			t.Errorf("ValidateOutputFile(%q) = (%q, %v), want (%q, nil)", validPath, got, err, validPath)
		}
	}
	for _, badPath := range []string{"", "   ", "bad\n.hcl", "bad\\path.hcl", "/abs.hcl", "dir/", "C:out.hcl", ".", "..", "a/../b.hcl"} {
		if _, err := ValidateOutputFile(badPath); err == nil {
			t.Errorf("ValidateOutputFile(%q): expected error, got nil", badPath)
		}
	}

	// 3. formatPostgresSQLDefaultValue "[]" -> "'{}'", BuildPostgresAlterTableSQL normalized default comparison,
	//    and no duplicate idx.Ops when single-part Opclass set.
	arrCol := PostgresColumnDescriptor{ColumnName: "tags", SQLType: "TEXT[]", HCLType: "text[]", HasDefaultValue: true, DefaultValue: "[]"}
	if got := formatPostgresSQLDefaultValue(arrCol); got != "'{}'" {
		t.Errorf("formatPostgresSQLDefaultValue([]) = %q, want \"'{}'\"", got)
	}
	beforeAlter := PostgresTableDescriptor{
		TableName:         "alter_def_tbl",
		DatabaseScope:     "PUBLIC",
		PrimaryKeyColumns: []string{"id"},
		Columns: []PostgresColumnDescriptor{
			{ColumnName: "id", SQLType: "UUID", HCLType: "uuid", IsPrimaryKey: true},
			{ColumnName: "tags", SQLType: "TEXT[]", HCLType: "text[]", HasDefaultValue: true, DefaultValue: "[]"},
			{ColumnName: "created_at", SQLType: "TIMESTAMPTZ", HCLType: "timestamptz", ServerDefault: "NOW()"},
		},
	}
	afterAlter := PostgresTableDescriptor{
		TableName:         "alter_def_tbl",
		DatabaseScope:     "PUBLIC",
		PrimaryKeyColumns: []string{"id"},
		Columns: []PostgresColumnDescriptor{
			{ColumnName: "id", SQLType: "UUID", HCLType: "uuid", IsPrimaryKey: true},
			{ColumnName: "tags", SQLType: "TEXT[]", HCLType: "text[]", HasDefaultValue: true, DefaultValue: "{}"},
			{ColumnName: "created_at", SQLType: "TIMESTAMPTZ", HCLType: "timestamptz", ServerDefault: "now()"},
		},
		Indexes: []PostgresIndexDescriptor{
			{
				Name:  "idx_tags_ops",
				Using: "GIN",
				Ops:   "gin_trgm_ops",
				Parts: []PostgresIndexPartDescriptor{{Column: "tags", Opclass: "gin_trgm_ops"}},
			},
		},
	}
	alterStmts, err := BuildPostgresAlterTableSQL(beforeAlter, afterAlter, "")
	if err != nil {
		t.Fatalf("BuildPostgresAlterTableSQL failed: %v", err)
	}
	if len(alterStmts) != 1 || strings.Contains(alterStmts[0], "ALTER COLUMN") || strings.Contains(alterStmts[0], "gin_trgm_ops gin_trgm_ops") {
		t.Fatalf("unexpected alter statements: %v", alterStmts)
	}
	createSQL := afterAlter.BuildPostgresCreateTableSQL("")
	if strings.Contains(createSQL, "gin_trgm_ops gin_trgm_ops") {
		t.Errorf("BuildPostgresCreateTableSQL duplicated opclass:\n%s", createSQL)
	}

	// 4. Atlas HCL type/opclass formatting.
	if got := formatPostgresHCLType("vector(768)"); got != `sql("vector(768)")` {
		t.Errorf("formatPostgresHCLType(vector(768)) = %q", got)
	}
	if got := formatPostgresAtlasHCLOpclass("gin_trgm_ops"); got != `"gin_trgm_ops"` {
		t.Errorf("extension opclass should be quoted, got %q", got)
	}
	if got := formatPostgresAtlasHCLOpclass("text_pattern_ops"); got != `text_pattern_ops` {
		t.Errorf("builtin opclass should stay unquoted, got %q", got)
	}

	// 5. Legacy wire 50201 table_name-only table must not be skipped (FromLegacyDorisExt).
	legacyFDP := stage1aSingleMsgFDP("legacy_50201.proto", "review.v1", "example.com/review/v1;reviewv1", &descriptorpb.DescriptorProto{
		Name: proto.String("LegacyOnlyTable"),
		Options: func() *descriptorpb.MessageOptions {
			mo := &descriptorpb.MessageOptions{}
			mo.ProtoReflect().SetUnknown(buildAzraTableMetaWire("legacy_only_tbl", 0, 0, nil, nil, "", "", nil))
			return mo
		}(),
		Field: []*descriptorpb.FieldDescriptorProto{
			stage1aScalarField("id", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, nil),
		},
	})
	legacyGen := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{legacyFDP}}, []string{"legacy_50201.proto"})
	legacyMeta := extractDorisTableMeta(legacyGen.Files[0].Messages[0])
	if legacyMeta == nil || !legacyMeta.FromLegacyDorisExt || !legacyMeta.hasDorisSpecificFields() {
		t.Fatalf("expected FromLegacyDorisExt=true and hasDorisSpecificFields=true, got %+v", legacyMeta)
	}
	legacyTbls, err := buildDorisTablesForFile(legacyGen.Files[0], &Options{})
	if err != nil || len(legacyTbls) != 1 {
		t.Fatalf("expected 1 Doris table from legacy 50201 table_name-only, got (%d, %v)", len(legacyTbls), err)
	}

	// 6. StarRocks PRIMARY KEY partition gating & Buckets / AutoBuckets & effectiveDorisPhysicalPartitionCols.
	srTbl := DorisTableDescriptor{
		TableName:          "sr_monthly_pk",
		DatabaseScope:      "TENANT",
		SyncTier:           "MONTHLY_PARTITIONED",
		UniqueKeyColumns:   []string{"tenant_id", "id"},
		PartitionByColumns: []string{"event_month"}, // NOT subset of UKs -> physical PARTITION BY omitted!
		Buckets:            16,
		Columns: []DorisColumnDescriptor{
			{ColumnName: "tenant_id", DorisType: "VARCHAR(64)", IsUniqueKey: true},
			{ColumnName: "id", DorisType: "VARCHAR(64)", IsUniqueKey: true},
			{ColumnName: "event_month", DorisType: "VARCHAR(16)"},
		},
	}
	srDDL := srTbl.BuildStarRocksCreateTableSQL("acme", 0)
	if strings.Contains(srDDL, "PARTITION BY") {
		t.Errorf("StarRocks PRIMARY KEY should omit PARTITION BY when partition col not in PK:\n%s", srDDL)
	}
	if !strings.Contains(srDDL, "BUCKETS 16") {
		t.Errorf("StarRocks DDL should honor t.Buckets=16:\n%s", srDDL)
	}
	if got := srTbl.BuildStarRocksAtlasHCL("acme", 0); strings.Contains(got, "partition_by {") {
		t.Errorf("StarRocks HCL should omit partition_by when partition col not in PK:\n%s", got)
	}
	srAuto := srTbl
	srAuto.Buckets = 0
	srAuto.AutoBuckets = true
	if sqlAuto := srAuto.BuildDorisCreateTableSQL("acme", 0); !strings.Contains(sqlAuto, "BUCKETS AUTO") {
		t.Errorf("Doris DDL should honor AutoBuckets=true:\n%s", sqlAuto)
	}
	srTblAfter := srTbl
	srTblAfter.SyncTier = "WHOLE_TABLE"
	srTblAfter.PartitionByColumns = nil
	if _, err := BuildStarRocksAlterTableSQL(srTbl, srTblAfter, "acme"); err != nil {
		t.Errorf("BuildStarRocksAlterTableSQL with unchanged empty physical partition should succeed: %v", err)
	}

	// 7. Recursive Iceberg type normalization & explicit iceberg_type precedence (host + runtime).
	iceCases := [][3]string{
		{"DATETIME(6)", "", "DATETIME"},
		{"DATETIMEV2(3)", "", "DATETIME"},
		{"TINYINT", "", "INT"},
		{"SMALLINT", "", "INT"},
		{"TEXT", "", "STRING"},
		{"ARRAY<SMALLINT>", "", "ARRAY<INT>"},
		{"", "LIST<SMALLINT>", "ARRAY<INT>"},
		{"MAP<TEXT, SMALLINT>", "", "MAP<STRING, INT>"},
		{"DECIMALV3(10,2)", "", "DECIMAL(10, 2)"},
		{"", "DATETIME(6)", "DATETIME"},
	}
	for _, c := range iceCases {
		if got := dorisColumnIcebergDDLType(c[0], c[1]); got != c[2] {
			t.Errorf("dorisColumnIcebergDDLType(%q, %q) = %q, want %q", c[0], c[1], got, c[2])
		}
	}

	// Explicit iceberg_type without doris_type overrides inferred type, and host/runtime Iceberg DDL match.
	iceProtoFDP := stage1aSingleMsgFDP("ice_override.proto", "review.v1", "example.com/ice/v1;icev1", &descriptorpb.DescriptorProto{
		Name: proto.String("IceOverride"),
		Options: stage1aMsgOpts(&proto2typepb.TableOptions{
			TableName:     "ice_override_tbl",
			DatabaseScope: "TENANT",
			SyncTier:      "ICEBERG_ONLY",
		}),
		Field: []*descriptorpb.FieldDescriptorProto{
			stage1aScalarField("id", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, stage1aColOpts(&proto2typepb.ColumnOptions{IcebergType: "VARCHAR(64)"}, nil)),
			stage1aScalarField("ext_num", 2, descriptorpb.FieldDescriptorProto_TYPE_INT32, false, stage1aColOpts(&proto2typepb.ColumnOptions{IcebergType: "LONG"}, nil)),
			stage1aScalarField("part_dt", 3, descriptorpb.FieldDescriptorProto_TYPE_STRING, true, stage1aColOpts(&proto2typepb.ColumnOptions{IsPartitionKey: true, DorisNullable: proto.Bool(true), IcebergType: "LIST<SMALLINT>"}, nil)),
		},
	})
	iceGen := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{iceProtoFDP}}, []string{"ice_override.proto"})
	if err := runner.Generate(iceGen, &Options{Lang: "go", Backend: "doris", DorisMode: "go+ddl"}); err != nil {
		t.Fatalf("Generate Iceberg override failed: %v", err)
	}
	iceFiles := stage1aGeneratedFiles(t, iceGen)
	iceSQL := stage1aFindBySuffix(t, iceFiles, "_iceberg.sql")
	if !strings.Contains(iceSQL, "ext_num BIGINT NOT NULL") || !strings.Contains(iceSQL, "part_dt ARRAY<INT>") {
		t.Errorf("unexpected Iceberg SQL:\n%s", iceSQL)
	}
	// Verify runtime emitted Go code compiles and computes identical Iceberg & StarRocks results.
	iceGo := stage1aFindBySuffix(t, iceFiles, "_doris.type.go")
	if !strings.Contains(iceGo, "func dorisSplitIcebergTopLevelComma") || !strings.Contains(iceGo, "func (t DorisTableDescriptor) BuildStarRocksCreateTableSQL") {
		t.Fatalf("expected updated runtime helpers in generated _doris.type.go")
	}

	// Verify runtime Go dorisColumnIcebergDDLType & StarRocks PK partition & Buckets parity cleanly.
	tmpRuntimeDir, err := os.MkdirTemp(".", "stage1a_doris_runtime_*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpRuntimeDir) }()
	rtGen := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{iceProtoFDP}}, []string{"ice_override.proto"})
	rtG := rtGen.NewGeneratedFile("runtime_only.go", "example.com/rt")
	rtG.P("package rt")
	rtG.P()
	emitDorisSharedRuntimeTypes(rtG)
	rtSrc := stage1aGeneratedFiles(t, rtGen)["runtime_only.go"]
	if err := os.WriteFile(filepath.Join(tmpRuntimeDir, "runtime_only.go"), []byte(rtSrc), 0o644); err != nil {
		t.Fatalf("write runtime_only.go: %v", err)
	}
	rtTest := `package rt
import (
	"strings"
	"testing"
)
func TestRuntimeFixes(t *testing.T) {
	if got := dorisColumnIcebergDDLType("MAP<TEXT, SMALLINT>", ""); got != "MAP<STRING, INT>" {
		t.Fatalf("runtime dorisColumnIcebergDDLType map = %q", got)
	}
	if got := dorisColumnIcebergDDLType("", "LIST<SMALLINT>"); got != "ARRAY<INT>" {
		t.Fatalf("runtime dorisColumnIcebergDDLType list = %q", got)
	}
	sr := DorisTableDescriptor{
		TableName: "sr_pk", DatabaseScope: "TENANT", SyncTier: "MONTHLY_PARTITIONED",
		UniqueKeyColumns: []string{"tenant_id", "id"}, PartitionByColumns: []string{"event_month"}, AutoBuckets: true,
		Columns: []DorisColumnDescriptor{
			{ColumnName: "tenant_id", DorisType: "VARCHAR(64)", IsUniqueKey: true},
			{ColumnName: "id", DorisType: "VARCHAR(64)", IsUniqueKey: true},
			{ColumnName: "event_month", DorisType: "VARCHAR(16)"},
		},
	}
	sql := sr.BuildStarRocksCreateTableSQL("acme", 0)
	if strings.Contains(sql, "PARTITION BY") || !strings.Contains(sql, "BUCKETS AUTO") {
		t.Fatalf("unexpected runtime StarRocks SQL:\n%s", sql)
	}
}
`
	if err := os.WriteFile(filepath.Join(tmpRuntimeDir, "runtime_only_test.go"), []byte(rtTest), 0o644); err != nil {
		t.Fatalf("write runtime_only_test.go: %v", err)
	}
	cmd := exec.Command("go", "test", "-v", ".")
	cmd.Dir = tmpRuntimeDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("runtime go test failed: %v\n%s", err, string(out))
	}

	// 8. Reject doris_nullable=true on Doris key/sequence/partition columns when SyncTier != ICEBERG_ONLY.
	badNullableFDP := stage1aSingleMsgFDP("bad_doris_null.proto", "review.v1", "example.com/review/v1;reviewv1", &descriptorpb.DescriptorProto{
		Name: proto.String("BadDorisNull"),
		Options: stage1aMsgOpts(&proto2typepb.TableOptions{
			TableName:        "bad_doris_null",
			DatabaseScope:    "TENANT",
			SyncTier:         "WHOLE_TABLE",
			UniqueKeyColumns: []string{"id"},
		}),
		Field: []*descriptorpb.FieldDescriptorProto{
			stage1aScalarField("id", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, stage1aColOpts(&proto2typepb.ColumnOptions{DorisType: "VARCHAR(64)", DorisNullable: proto.Bool(true)}, nil)),
		},
	})
	if err := runner.Generate(newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{badNullableFDP}}, []string{"bad_doris_null.proto"}), &Options{Lang: "go", Backend: "doris", DorisMode: "ddl"}); err == nil {
		t.Errorf("expected error for doris_nullable=true on Doris UNIQUE KEY column")
	}
	badPartNullableFDP := stage1aSingleMsgFDP("bad_doris_part_null.proto", "review.v1", "example.com/review/v1;reviewv1", &descriptorpb.DescriptorProto{
		Name: proto.String("BadDorisPartNull"),
		Options: stage1aMsgOpts(&proto2typepb.TableOptions{
			TableName:          "bad_doris_part_null",
			DatabaseScope:      "TENANT",
			SyncTier:           "MONTHLY_PARTITIONED",
			UniqueKeyColumns:   []string{"id", "event_month"},
			PartitionByColumns: []string{"event_month"},
		}),
		Field: []*descriptorpb.FieldDescriptorProto{
			stage1aScalarField("id", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, stage1aColOpts(&proto2typepb.ColumnOptions{DorisType: "VARCHAR(64)"}, nil)),
			stage1aScalarField("event_month", 2, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, stage1aColOpts(&proto2typepb.ColumnOptions{DorisType: "VARCHAR(16)", DorisNullable: proto.Bool(true)}, nil)),
		},
	})
	if err := runner.Generate(newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{badPartNullableFDP}}, []string{"bad_doris_part_null.proto"}), &Options{Lang: "go", Backend: "doris", DorisMode: "ddl"}); err == nil {
		t.Errorf("expected error for doris_nullable=true on Doris partition column")
	}
	badSeqNullableFDP := stage1aSingleMsgFDP("bad_doris_seq_null.proto", "review.v1", "example.com/review/v1;reviewv1", &descriptorpb.DescriptorProto{
		Name: proto.String("BadDorisSeqNull"),
		Options: stage1aMsgOpts(&proto2typepb.TableOptions{
			TableName:        "bad_doris_seq_null",
			DatabaseScope:    "TENANT",
			SyncTier:         "WHOLE_TABLE",
			UniqueKeyColumns: []string{"id"},
			SequenceCol:      "seq_ver",
		}),
		Field: []*descriptorpb.FieldDescriptorProto{
			stage1aScalarField("id", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, stage1aColOpts(&proto2typepb.ColumnOptions{DorisType: "VARCHAR(64)"}, nil)),
			stage1aScalarField("seq_ver", 2, descriptorpb.FieldDescriptorProto_TYPE_INT64, false, stage1aColOpts(&proto2typepb.ColumnOptions{DorisNullable: proto.Bool(true)}, nil)),
		},
	})
	if err := runner.Generate(newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{badSeqNullableFDP}}, []string{"bad_doris_seq_null.proto"}), &Options{Lang: "go", Backend: "doris", DorisMode: "ddl"}); err == nil {
		t.Errorf("expected error for doris_nullable=true on Doris sequence column")
	}

	// 9. Dual-annotated Postgres-only columns must not leak into Doris tables.
	dualFDP := stage1aSingleMsgFDP("dual_no_leak.proto", "review.v1", "example.com/review/v1;reviewv1", &descriptorpb.DescriptorProto{
		Name: proto.String("DualNoLeak"),
		Options: stage1aMsgOpts(&proto2typepb.TableOptions{
			TableName:        "dual_no_leak",
			DatabaseScope:    "TENANT",
			SyncTier:         "WHOLE_TABLE",
			UniqueKeyColumns: []string{"id"},
		}),
		Field: []*descriptorpb.FieldDescriptorProto{
			stage1aScalarField("id", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, stage1aColOpts(&proto2typepb.ColumnOptions{DorisType: "VARCHAR(64)"}, nil)),
			stage1aScalarField("pg_only", 2, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, stage1aColOpts(&proto2typepb.ColumnOptions{SqlType: "UUID", ColumnName: "pg_col", DefaultValue: proto.String("")}, nil)),
		},
	})
	dualGen := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{dualFDP}}, []string{"dual_no_leak.proto"})
	if err := runner.Generate(dualGen, &Options{Lang: "go", Backend: "doris", DorisMode: "ddl"}); err != nil {
		t.Fatalf("dual no leak Generate failed: %v", err)
	}
	if dorisSQL := stage1aFindBySuffix(t, stage1aGeneratedFiles(t, dualGen), "_doris.sql"); strings.Contains(dorisSQL, "pg_col") || strings.Contains(dorisSQL, "pg_only") {
		t.Errorf("Postgres-only column leaked into Doris DDL:\n%s", dorisSQL)
	}

	// 10. postgresIndexSuffix hashes ordered Parts tuple & mergeColumnMeta sets HasDefaultValue.
	s1 := postgresIndexSuffix(PostgresIndexDescriptor{Parts: []PostgresIndexPartDescriptor{{Column: "a", Desc: true}, {Column: "b"}}})
	s2 := postgresIndexSuffix(PostgresIndexDescriptor{Parts: []PostgresIndexPartDescriptor{{Column: "a", Desc: false}, {Column: "b"}}})
	s3 := postgresIndexSuffix(PostgresIndexDescriptor{Parts: []PostgresIndexPartDescriptor{{Column: "b"}, {Column: "a", Desc: true}}})
	if s1 == s2 || s1 == s3 {
		t.Errorf("expected distinct index suffixes for distinct order/desc, got s1=%q s2=%q s3=%q", s1, s2, s3)
	}
	dstMeta := &DorisColumnMetaIR{}
	srcMeta := &DorisColumnMetaIR{DefaultValue: "x"}
	mergeColumnMeta(dstMeta, srcMeta)
	if !dstMeta.HasDefaultValue || dstMeta.DefaultValue != "x" {
		t.Errorf("expected mergeColumnMeta to set HasDefaultValue=true, got %+v", dstMeta)
	}
}
