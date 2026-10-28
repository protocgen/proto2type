# Apache Doris 4.x, StarRocks 4.x & Apache Iceberg Lakehouse Guide (`backend=doris` / `backend=starrocks`)

`proto2type` provides first-class Go storage structs, SQL DDL, Atlas HCL schemas, and schema evolution linting for **Apache Doris 4.x**, **StarRocks 4.x**, and **Apache Iceberg v2** lakehouse tables from a single Protocol Buffer definition.

---

## Quick Start (`buf.gen.yaml`)

```yaml
version: v2
plugins:
  - local: protoc-gen-proto2type
    out: gen/go
    opt:
      - paths=source_relative
      - lang=go
      - backend=doris
      - doris_mode=all               # "go" (default), "ddl", "hcl", "both", "all", "starrocks", "starrocks_ddl", "starrocks_hcl", "starrocks_both"
      - doris_storage_vault=s3_vault # optional default Doris storage vault / StarRocks storage volume
      - iceberg_catalog=iceberg      # optional default Iceberg catalog (default: "iceberg")
```

### Plugin Options

| Option | Values | Default | Description |
|---|---|---|---|
| `backend` | `doris`, `starrocks` | — | Enables the Apache Doris 4.x, StarRocks 4.x & Apache Iceberg lakehouse generator |
| `doris_mode` | `go`, `ddl`, `hcl`, `both`, `all`, `starrocks`, `starrocks_ddl`, `starrocks_hcl`, `starrocks_both` | `go` (`backend=doris`) / `starrocks` (`backend=starrocks`) | Controls which artifacts (`_doris.type.go`, `_doris.sql`, `_iceberg.sql`, `_starrocks.sql`, `_doris.hcl`, `_iceberg.hcl`, `_starrocks.hcl`) are emitted |
| `doris_storage_vault` | SQL identifier | `""` | Default Doris `"storage_vault_name"` / StarRocks `"storage_volume"` property when not set on `(proto2type.message).table` |
| `iceberg_catalog` | SQL identifier | `"iceberg"` | Default Iceberg catalog prefix (`<catalog>.<namespace>.<table>`) when not set on `(proto2type.message).table` |

---

## Protobuf Annotations

Import `proto2type/options.proto` and annotate messages with `(proto2type.message).table` and fields with `(proto2type.field).column`.

> **Legacy Wire Compatibility**: `proto2type` also automatically decodes wire-level table metadata on extension `50201` and column metadata on extension `50200` without requiring changes to existing `.proto` descriptors.

```protobuf
syntax = "proto3";
package clinical.v1;

import "google/protobuf/timestamp.proto";
import "google/type/date.proto";
import "proto2type/options.proto";

message DocumentCitation {
  int32 page_number = 1 [(proto2type.field).column = {
    column_name: "citation_page_number"
    doris_type: "INT"
    iceberg_type: "INT"
  }];
  repeated float bbox_coords = 2 [(proto2type.field).column = {
    column_name: "citation_bbox_coords"
    doris_type: "ARRAY<FLOAT>"
    iceberg_type: "LIST<FLOAT>"
  }];
}

message EligibilityCriterionResult {
  option (proto2type.message).table = {
    table_name: "eligibility_criterion_results"
    database_scope: "TENANT"
    sync_tier: "MONTHLY_PARTITIONED"
    unique_key_columns: ["patient_id", "criterion_id"]
    partition_by_columns: ["event_month"]
    colocate_group: "patient_cg"
    sequence_col: "publication_generation"
    inverted_index_columns: ["all_mrns"]
    ngram_bf_columns: ["criterion_id"]
    bloom_filter_columns: ["patient_id"]
    distributed_by_column: "patient_id"
    file_cache_ttl_seconds: 604800
    storage_vault_name: "lakehouse_vault"
    iceberg_catalog: "lakehouse"
  };

  string patient_id = 1 [(proto2type.field).column = {
    doris_type: "VARCHAR(64)"
    iceberg_type: "STRING"
    is_unique_key: true
  }];
  string criterion_id = 2 [(proto2type.field).column = {
    doris_type: "VARCHAR(64)"
    iceberg_type: "STRING"
    is_unique_key: true
  }];
  repeated string all_mrns = 3 [(proto2type.field).column = {
    doris_type: "ARRAY<VARCHAR(64)>"
    iceberg_type: "LIST<STRING>"
    inverted_index: true
  }];
  google.type.Date event_month = 4 [(proto2type.field).column = {
    doris_type: "DATEV2"
    iceberg_type: "DATE"
    is_partition_key: true
  }];
  google.protobuf.Timestamp verified_at = 5 [(proto2type.field).column = {
    doris_type: "DATETIMEV2(6)"
    iceberg_type: "TIMESTAMPTZ"
  }];
  DocumentCitation citation = 6 [(proto2type.field).column = {
    inline_nested: true
  }];
  int64 publication_generation = 7 [(proto2type.field).column = {
    doris_type: "BIGINT"
    iceberg_type: "LONG"
    is_sequence_col: true
    default_value: "0"
  }];
}
```

### `TableOptions` Reference (`(proto2type.message).table`)

| Field | Number | Type | Description |
|---|---|---|---|
| `table_name` | `1` | `string` | Physical Doris, StarRocks, and Iceberg SQL table name (required) |
| `database_scope` | `2` | `string` | `TENANT`, `GLOBAL_TERMINOLOGY`, `GLOBAL_TERMINOLOGY_RAG`, or `GLOBAL_TELEMETRY` |
| `sync_tier` | `3` | `string` | `WHOLE_TABLE`, `MONTHLY_PARTITIONED`, `RELEASE_PARTITIONED`, `INTERNAL_ONLY`, or `ICEBERG_ONLY` |
| `unique_key_columns` | `4` | `repeated string` | Ordered `UNIQUE KEY (...)` (Doris MoW) / `PRIMARY KEY (...)` (StarRocks Persistent Index) columns |
| `partition_by_columns` | `5` | `repeated string` | Partition columns (`PARTITION BY RANGE` / `PARTITION BY LIST`) |
| `colocate_group` | `6` | `string` | `"colocate_with"` group name |
| `sequence_col` | `7` | `string` | `"function_column.sequence_col"` column for out-of-order upsert deduplication |
| `inverted_index_columns` | `8` | `repeated string` | Columns with Doris `USING INVERTED` / StarRocks `USING GIN` or `USING BITMAP` indexes |
| `ngram_bf_columns` | `9` | `repeated string` | Columns with Doris `USING NGRAM_BF` / StarRocks `USING NGRAMBF` indexes |
| `bloom_filter_columns` | `10` | `repeated string` | Doris/StarRocks `"bloom_filter_columns"` & Iceberg `"write.parquet.bloom-filter-enabled.column.<col>"` |
| `distributed_by_column` | `11` | `string` | Column for `DISTRIBUTED BY HASH(<col>)` (defaults to first key column) |
| `duplicate_key_columns` | `12` | `repeated string` | Ordered `DUPLICATE KEY (...)` columns for append/vector/log tables |
| `file_cache_ttl_seconds` | `13` | `int32` | Doris `"file_cache_ttl_seconds"` property |
| `storage_vault_name` | `14` | `string` | Per-table override for Doris `"storage_vault_name"` / StarRocks `"storage_volume"` |
| `iceberg_catalog` | `15` | `string` | Per-table override for Iceberg catalog name |
| `embedding_dimension` | `16` | `int32` | Positive vector dimension for HNSW `USING ANN` (Doris) / `USING VECTOR` (StarRocks) index. Requires a `DUPLICATE KEY` table (`unique_key_columns` empty) with a non-nullable `ARRAY<FLOAT>` column named `embedding` |

### `ColumnOptions` Reference (`(proto2type.field).column`)

| Field | Number | Type | Description |
|---|---|---|---|
| `doris_type` | `1` | `string` | Explicit Doris SQL type (`VARCHAR(64)`, `DATEV2`, `DATETIMEV2(6)`, `LARGEINT`, `ARRAY<VARCHAR(64)>`, `JSONB`, `VARIANT`, etc.) |
| `iceberg_type` | `2` | `string` | Explicit Iceberg spec/DDL type (`STRING`, `LONG`, `TIMESTAMPTZ`, `LIST<STRING>`, etc.) |
| `inverted_index` | `3` | `bool` | Emit a Doris `USING INVERTED` / StarRocks `USING GIN` or `USING BITMAP` index on this column |
| `is_unique_key` | `4` | `bool` | Include this column in `UNIQUE KEY` / `PRIMARY KEY` |
| `is_sequence_col` | `5` | `bool` | Mark this column as `"function_column.sequence_col"` |
| `inline_nested` | `6` | `bool` | Recursively flatten child message fields into the parent table |
| `is_partition_key` | `7` | `bool` | Include this column in `PARTITION BY` |
| `strip_enum_prefix` | `8` | `bool` | Strip the proto enum's screaming-snake prefix before storing in `VARCHAR` / `ARRAY<VARCHAR>` |
| `column_name` | `9` | `string` | Override the physical SQL column name |
| `default_value` | `10` | `string` | Explicit SQL `DEFAULT "..."` literal |

---

## Generated Go API (`<name>_doris.type.go`)

For each annotated message `<Msg>`, `proto2type` generates:

1. **`type <Msg>Doris struct`**:
   - Flattened Go struct with `db:"<col>"` and `doris:"<DORIS_TYPE>"` struct tags.
   - `uint64` columns mapped to `LARGEINT` (`DECIMAL(38, 0)` in Iceberg) use `DorisUint64` / `DorisUint64Array` to preserve the full unsigned 64-bit range `[0, math.MaxUint64]` without `int64` overflow.
   - `repeated` columns use `DorisStringArray`, `DorisInt32Array`, `DorisInt64Array`, `DorisUint64Array`, `DorisFloat32Array`, `DorisFloat64Array`, or `DorisBoolArray`, supporting both standard JSON arrays (`["a", "b"]`) and Apache Doris single-quoted wire arrays (`['a', 'b']`).

2. **Row Scanners & Proto Converters**:
   - `func (<Msg>Doris) TableName() string`
   - `func (<Msg>Doris) Columns() []string`
   - `func (r *<Msg>Doris) Values() []any`
   - `func (r *<Msg>Doris) ScanRow(scanner DorisScanner) error`
   - `func (r *<Msg>Doris) ToProto() *<Msg>`
   - `func (r *<Msg>Doris) FromProto(m *<Msg>)`
   - `func <Msg>ProtoValues(m *<Msg>) []any`
   - `func Scan<Msg>Proto(scanner DorisScanner) (*<Msg>, error)`
   - `func NewDorisValueScanner(values []any) DorisScanner` (for in-memory round-trip testing)

3. **Table Descriptors, SQL DDL & Atlas HCL Builders**:
   - `func <Msg>DorisTable() DorisTableDescriptor`
   - `func (r <Msg>Doris) Descriptor() DorisTableDescriptor`
   - `func PackageDorisTables() []DorisTableDescriptor`
   - `func (t DorisTableDescriptor) BuildDorisCreateTableSQL(tenantSlug string, buckets int) string`
   - `func (t DorisTableDescriptor) BuildStarRocksCreateTableSQL(tenantSlug string, buckets int) string`
   - `func (t DorisTableDescriptor) BuildIcebergCreateTableSQL(tenantSlug string) string`
   - `func (t DorisTableDescriptor) BuildDorisAtlasHCL(tenantSlug string, buckets int) string`
   - `func (t DorisTableDescriptor) BuildStarRocksAtlasHCL(tenantSlug string, buckets int) string`
   - `func (t DorisTableDescriptor) BuildIcebergAtlasHCL(tenantSlug string) string`
   - `func ParseDorisAtlasHCL(hclContent string) ([]DorisTableDescriptor, error)`
   - `func (c DorisColumnDescriptor) CanonicalIcebergSpecType() string`

---

## Schema Evolution Linter (`DORIS001` – `DORIS006`)

Both the `generator` package and every generated `<name>_doris.type.go` package export a deterministic schema evolution linter that validates compatibility between `before` and `after` `DorisTableDescriptor` definitions across Apache Doris 4.x Light Schema Change (LSC), StarRocks 4.x, and Apache Iceberg v2:

| Rule | Constant | Enforced Invariant |
|---|---|---|
| `DORIS001_IMMUTABLE_KEY` | `DorisRuleImmutableKey` | `UNIQUE KEY` and `DUPLICATE KEY` column lists cannot be mutated on an existing table |
| `DORIS002_IMMUTABLE_SEQUENCE` | `DorisRuleImmutableSequence` | `sequence_col` cannot be mutated on an existing table |
| `DORIS003_IMMUTABLE_DISTRIBUTION` | `DorisRuleImmutableDistribution` | `distributed_by_column` / `DistributionColumns` and `colocate_group` cannot be mutated on an existing table |
| `DORIS004_NO_COLUMN_DROP` | `DorisRuleNoColumnDrop` | Existing columns cannot be dropped from an existing table |
| `DORIS005_NON_NULL_SCALAR_DEFAULT` | `DorisRuleNonNullScalarDefault` | Newly added `NOT NULL` scalar columns must declare a non-empty `DefaultValue` |
| `DORIS006_SAFE_TYPE_EVOLUTION` | `DorisRuleSafeTypeEvolution` | Existing column types may only widen (`VARCHAR(m) -> VARCHAR(n)` with `n >= m` or `STRING`/`TEXT`, `DATE/DATEV2 -> DATETIME/DATETIMEV2(p)`, `DATETIME(p1) -> DATETIME(p2)` with `p2 >= p1`, `DECIMAL(p1,s) -> DECIMAL(p2,s)` with `p2 >= p1`, `ARRAY<T1> -> ARRAY<T2>`, `TINYINT -> SMALLINT/INT/BIGINT/LARGEINT`, `SMALLINT -> INT/BIGINT/LARGEINT`, `INT -> BIGINT/LARGEINT`, `BIGINT -> LARGEINT`, `FLOAT -> DOUBLE`) |

```go
// Validate evolving a single table descriptor:
if err := ValidateDorisTableEvolution(beforeTable, afterTable); err != nil {
    log.Fatalf("unsafe schema evolution:\n%v", err)
}

// Or inspect structured violations across an entire package or parsed Atlas HCL schema:
violations := LintDorisSchemaEvolution(oldTables, PackageDorisTables())
for _, v := range violations {
    fmt.Println(v.Rule, v.TableName, v.ColumnName, v.Message)
}
```
