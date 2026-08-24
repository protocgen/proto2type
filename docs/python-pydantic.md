# Python and Pydantic Support

`proto2type` generates native Python Pydantic v2 models from your Protocol Buffer definitions. It converts proto types to idiomatic Python types (`datetime`, `timedelta`, `dict`, `list`), applies `buf.validate` constraints as Pydantic `Field()` arguments, and provides seamless JSON serialization out of the box.

## Quick Start

Add the plugin to your `buf.gen.yaml`:

```yaml
version: v2
plugins:
  - local: protoc-gen-proto2type
    out: gen/python
    opt:
      - lang=python
```

Then run `buf generate`.

## Options Reference

| Option | Default | Description |
|---|---|---|
| `python_base_class` | `BaseModel` | Python: custom Pydantic base class |
| `python_alias_generator` | _(none)_ | Python: alias generator (e.g. `camel` for `to_camel`) |
| `python_enum_style` | _(none)_ | Python: enum style (e.g. `raw` to preserve proto names) |
| `python_preset` | _(none)_ | Python: preset (e.g. `a2a` to apply A2A defaults) |
| `python_description` | _(none)_ | Python: module-level docstring |
| `python_strip_proto_suffix` | `false` | Python: use `.py` instead of `_pb2_pydantic.py` |

## Generated Code Walkthrough

By default, the plugin generates standard Pydantic models. Optional fields and nested types default to `None`, while standard proto types receive their zero values.

### Models and Fields

```python
from pydantic import BaseModel, ConfigDict, Field
from datetime import datetime, timedelta

class User(BaseModel):
    """User represents a user account."""
    model_config = ConfigDict(populate_by_name=True)

    id: str = Field(default='', exclude=True)
    email: str = Field(..., pattern='^[^@]+@[^@]+\\.[^@]+$')
    active: bool = False
    age: int = Field(default=0, ge=0, le=150)
    roles: list[str] = Field(default_factory=list, min_length=1, max_length=10)
```

> **Note**: Models define `model_config = ConfigDict(populate_by_name=True)` to ensure keyword-escaped fields can be initialized via their original proto names.

### Enums

By default, enums are represented as `str, Enum` classes with lowercase values, omitting the `_UNSPECIFIED` default value.

```python
class UserStatus(str, Enum):
    """UserStatus represents the user's account status."""
    active = 'active'
    suspended = 'suspended'
    deleted = 'deleted'
```

If you specify `python_enum_style=raw`, the generated Enum preserves original casing and includes `UNSPECIFIED`:

```python
class UserStatus(str, Enum):
    USER_STATUS_UNSPECIFIED = 'USER_STATUS_UNSPECIFIED'
    USER_STATUS_ACTIVE = 'USER_STATUS_ACTIVE'
```

### Keyword Escaping

Fields named after Python reserved keywords (like `type`, `self`) are escaped with a trailing underscore and an explicit Pydantic `alias`.

```python
class KeywordFields(BaseModel):
    type_: str = Field(default='', alias='type')
    self_: int = Field(default=0, alias='self')
```

### Oneofs

Proto `oneof` fields are mapped to a Python `Union` type (using the `|` syntax).

```python
class Message(BaseModel):
    contact_method: str | int | None = None
```

## Validation Constraints

When `buf.validate` constraints are present, they are seamlessly converted into Pydantic `Field()` arguments.

| buf.validate constraint | Pydantic equivalent |
|---|---|
| `string.min_len` | `min_length` |
| `string.max_len` | `max_length` |
| `string.pattern` | `pattern` |
| `int32.gt` | `gt` |
| `int32.gte` | `ge` |
| `int32.lt` | `lt` |
| `int32.lte` | `le` |

Example generated constraints:

```python
email: str = Field(..., pattern='^[^@]+@[^@]+\\.[^@]+$')
display_name: str = Field(..., min_length=1, max_length=255)
age: int = Field(default=0, ge=0, le=150)
```

## Well-Known Types

Well-Known Types (WKTs) are natively mapped to idiomatic Python constructs.

| WKT | Python equivalent |
|---|---|
| `google.protobuf.Timestamp` | `datetime` |
| `google.protobuf.Duration` | `timedelta` |
| `google.protobuf.Struct` | `dict[str, Any]` |
| `google.protobuf.Value` | `Any` |
| `google.protobuf.ListValue` | `list[Any]` |
| `google.protobuf.Empty` | `None` |
| `google.protobuf.Any` | `Any` |
| `google.protobuf.FieldMask` | `list[str]` |
| `google.protobuf.BoolValue` | `bool \| None` |
| `google.protobuf.Int32Value` | `int \| None` |
| `google.protobuf.Int64Value` | `int \| None` |
| `google.protobuf.StringValue` | `str \| None` |
| `google.protobuf.DoubleValue` | `float \| None` |
| `google.protobuf.FloatValue` | `float \| None` |
| `google.protobuf.BytesValue` | `bytes \| None` |

## Serialization

`proto2type` ensures generated models match standard Protocol Buffer JSON encoding conventions via automatic `@field_serializer` hooks. 

### Timestamps

`datetime` values are converted to standard RFC 3339 strings on serialization.

```python
@field_serializer('created_at', 'deleted_at')
def _serialize_datetime(self, v: datetime, _info: object) -> str | None:
    # ... handles UTC timezone awareness and RFC 3339 stringification
```

### Bytes

`bytes` values are encoded as standard base64 strings.

```python
@field_serializer('avatar', 'avatar_thumbnail')
def _serialize_bytes(self, v: bytes, _info: object) -> str | None:
    # ... handles base64 encoding
```

## Presets

Applying `python_preset=a2a` configures the plugin for A2A integrations. This automatically sets `python_alias_generator=camel`, `python_enum_style=raw`, and additionally generates a `to_proto_json()` helper for accurate Protocol Buffer JSON formatting:

```python
class User(BaseModel):
    # ...
    def to_proto_json(self) -> dict:
        return self.model_dump(by_alias=True, exclude_none=True)
```

## Framework Integration

Because generated models are standard Pydantic classes, they work directly with modern Python data-validation tools.

### FastAPI

Use generated models as request or response schemas in FastAPI.

```python
from fastapi import FastAPI
from gen.user_pb2_pydantic import User

app = FastAPI()

@app.post("/users", response_model=User)
async def create_user(user: User):
    # Validates according to proto constraints
    return user
```

### LLM Structured Output (Instructor / LangChain)

Use your schema definitions as LLM tools to extract structured models.

```python
import instructor
from openai import OpenAI
from gen.user_pb2_pydantic import User

client = instructor.from_openai(OpenAI())

user_info = client.chat.completions.create(
    model="gpt-4o",
    response_model=User,
    messages=[{"role": "user", "content": "Extract data: John is active, age 32."}],
)
```

## Recursive Models

Self-referential and recursive proto messages are supported using Pydantic's `model_rebuild()` feature. 

```python
class Category(BaseModel):
    """Category is a recursive tree structure for testing model_rebuild() generation."""
    model_config = ConfigDict(populate_by_name=True)

    name: str = ''
    parent: Category | None = None
    children: list[Category] = Field(default_factory=list)

Category.model_rebuild()
```

## Known Limitations

- **Pydantic v2 Only**: Generated code relies on Pydantic v2-specific features (`model_config`, `field_serializer`, `model_rebuild`). Pydantic v1 is not supported.
- **CEL Expressions**: Advanced `buf.validate` expressions written in CEL are not translated to Python. Only standard constraint options map to Pydantic constraints.
- **Integer Precision**: Python's native `int` supports arbitrary precision. Generated models do not automatically validate that 32-bit or 64-bit bounds are strictly enforced (unless a standard `buf.validate` constraint specifies bounds).

> See [`../examples/GUIDE.md`](../examples/GUIDE.md) and [`../CONFIG.md`](../CONFIG.md) for more comprehensive configuration options.
