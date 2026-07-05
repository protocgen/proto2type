"""Runtime integration tests for proto2type Python/Pydantic generated code.

Validates that generated Pydantic models:
1. Import without error
2. Instantiate with valid data
3. Serialize to JSON correctly
4. Enforce constraints (reject invalid data)
5. Handle all field types (timestamps, bytes, enums, oneofs, maps, nested messages)

Exit 0 on success, non-zero on failure. Prints TAP-like output for Go test parsing.
"""

import json
import sys
import traceback
from datetime import datetime, timedelta, timezone
from pathlib import Path

# Add the golden gen directory to the import path.
gen_dir = Path(__file__).resolve().parent.parent.parent / "testdata" / "golden" / "python" / "gen"
sys.path.insert(0, str(gen_dir))

tests_run = 0
tests_passed = 0
tests_failed = 0


def test(name, fn):
    global tests_run, tests_passed, tests_failed
    tests_run += 1
    try:
        fn()
        tests_passed += 1
        print(f"ok {tests_run} - {name}")
    except Exception as e:
        tests_failed += 1
        print(f"not ok {tests_run} - {name}")
        traceback.print_exc()


# ---------------------------------------------------------------------------
# Test: All modules import successfully
# ---------------------------------------------------------------------------
def test_imports():
    from user_pb2_pydantic import User, Address, Tag, UserStatus
    from complex_pb2_pydantic import (
        Priority, Settings, SettingsTheme, Organization,
        OrganizationDepartment, OrganizationDepartmentTeam,
        Notification, Document, TreeNode, AuditLog, Event, WktPayload,
    )
    from catalog_pb2_pydantic import ModelCatalogEntry
    from keywords_pb2_pydantic import KeywordFields
    from options_pb2_pydantic import MessageOptions, FieldOptions
    from streaming_pb2_pydantic import StreamEvent


test("all modules import", test_imports)


# ---------------------------------------------------------------------------
# Test: User model instantiation with required fields
# ---------------------------------------------------------------------------
def test_user_required_fields():
    from user_pb2_pydantic import User
    u = User(email="a@b.com", display_name="Test User")
    assert u.email == "a@b.com"
    assert u.display_name == "Test User"
    assert u.active is False  # default
    assert u.age == 0  # default
    assert u.roles == []  # default_factory=list
    assert u.metadata == {}  # default_factory=dict


test("User required fields + defaults", test_user_required_fields)


# ---------------------------------------------------------------------------
# Test: User model rejects missing required fields
# ---------------------------------------------------------------------------
def test_user_missing_required():
    from user_pb2_pydantic import User
    from pydantic import ValidationError
    try:
        User()  # email and display_name are required
        raise AssertionError("Should have raised ValidationError")
    except ValidationError as e:
        errors = e.errors()
        missing_fields = {err["loc"][0] for err in errors}
        assert "email" in missing_fields, f"Expected 'email' in {missing_fields}"
        assert "display_name" in missing_fields, f"Expected 'display_name' in {missing_fields}"


test("User rejects missing required fields", test_user_missing_required)


# ---------------------------------------------------------------------------
# Test: buf/validate constraints enforced via Pydantic Field()
# ---------------------------------------------------------------------------
def test_user_validate_constraints():
    from user_pb2_pydantic import User
    from pydantic import ValidationError

    # display_name min_length=1: empty string should fail
    try:
        User(email="a@b.com", display_name="")
        raise AssertionError("Empty display_name should fail min_length=1")
    except ValidationError:
        pass

    # age ge=0, le=150: negative should fail
    try:
        User(email="a@b.com", display_name="X", age=-1)
        raise AssertionError("Negative age should fail ge=0")
    except ValidationError:
        pass

    # age ge=0: zero should pass (the gte:0 bug fix)
    u = User(email="a@b.com", display_name="X", age=0)
    assert u.age == 0

    # age le=150: 151 should fail
    try:
        User(email="a@b.com", display_name="X", age=151)
        raise AssertionError("Age 151 should fail le=150")
    except ValidationError:
        pass


test("buf/validate constraints enforced", test_user_validate_constraints)


# ---------------------------------------------------------------------------
# Test: OUTPUT_ONLY fields excluded from serialization
# ---------------------------------------------------------------------------
def test_output_only_excluded():
    from user_pb2_pydantic import User
    u = User(email="a@b.com", display_name="Test", id="secret-id")
    data = u.model_dump()
    assert "id" not in data, f"OUTPUT_ONLY 'id' should be excluded, got {data.keys()}"
    assert "created_at" not in data, "OUTPUT_ONLY 'created_at' should be excluded"


test("OUTPUT_ONLY fields excluded from serialization", test_output_only_excluded)


# ---------------------------------------------------------------------------
# Test: JSON serialization round-trip
# ---------------------------------------------------------------------------
def test_json_roundtrip():
    from user_pb2_pydantic import User, Address, Tag
    u = User(
        email="test@example.com",
        display_name="Test User",
        active=True,
        age=30,
        roles=["admin", "editor"],
        metadata={"env": "prod"},
        address=Address(street="123 Main", city="Springfield"),
        tags=[Tag(key="env", value="prod")],
    )
    json_str = u.model_dump_json()
    data = json.loads(json_str)
    assert data["email"] == "test@example.com"
    assert data["active"] is True
    assert data["age"] == 30
    assert data["roles"] == ["admin", "editor"]
    assert data["metadata"] == {"env": "prod"}
    assert data["address"]["street"] == "123 Main"
    assert len(data["tags"]) == 1
    assert data["tags"][0]["key"] == "env"


test("JSON serialization round-trip", test_json_roundtrip)


# ---------------------------------------------------------------------------
# Test: Enum fields
# ---------------------------------------------------------------------------
def test_enum_fields():
    from user_pb2_pydantic import User, UserStatus
    # Default enum should be None (UNSPECIFIED skipped)
    u = User(email="a@b.com", display_name="X")
    assert u.status is None

    # Set to a valid value
    u = User(email="a@b.com", display_name="X", status=UserStatus.active)
    assert u.status == UserStatus.active
    data = json.loads(u.model_dump_json())
    assert data["status"] == "active"


test("Enum fields", test_enum_fields)


# ---------------------------------------------------------------------------
# Test: Nested message as separate class
# ---------------------------------------------------------------------------
def test_nested_messages():
    from complex_pb2_pydantic import (
        Organization, OrganizationDepartment, OrganizationDepartmentTeam,
    )
    team = OrganizationDepartmentTeam(name="Backend")
    dept = OrganizationDepartment(name="Engineering", teams=[team])
    org = Organization(name="Acme", departments=[dept])
    data = json.loads(org.model_dump_json())
    assert data["name"] == "Acme"
    assert data["departments"][0]["name"] == "Engineering"
    assert data["departments"][0]["teams"][0]["name"] == "Backend"


test("Nested messages as top-level classes", test_nested_messages)


# ---------------------------------------------------------------------------
# Test: Self-referencing / recursive message
# ---------------------------------------------------------------------------
def test_recursive_message():
    from complex_pb2_pydantic import TreeNode
    leaf = TreeNode(value="leaf")
    root = TreeNode(value="root", children=[leaf])
    data = json.loads(root.model_dump_json())
    assert data["value"] == "root"
    assert data["children"][0]["value"] == "leaf"


test("Recursive/self-referencing message", test_recursive_message)


# ---------------------------------------------------------------------------
# Test: Oneof union types
# ---------------------------------------------------------------------------
def test_oneof():
    from complex_pb2_pydantic import Notification
    n = Notification(id="1", channel="email")
    assert n.channel == "email"
    data = json.loads(n.model_dump_json())
    assert data["channel"] == "email"


test("Oneof union types", test_oneof)


# ---------------------------------------------------------------------------
# Test: Datetime serialization (RFC 3339 with Z suffix)
# ---------------------------------------------------------------------------
def test_datetime_serialization():
    from user_pb2_pydantic import User
    dt = datetime(2025, 6, 15, 12, 30, 45, 123000, tzinfo=timezone.utc)
    u = User(email="a@b.com", display_name="X", created_at=dt)
    # created_at is OUTPUT_ONLY so it's excluded from model_dump,
    # but we can test the serializer directly
    serialized = u._serialize_datetime(dt, None)
    assert serialized == "2025-06-15T12:30:45.123Z", f"Got: {serialized}"


test("Datetime RFC 3339 serialization", test_datetime_serialization)


# ---------------------------------------------------------------------------
# Test: Datetime tz-aware non-UTC conversion
# ---------------------------------------------------------------------------
def test_datetime_tz_conversion():
    from user_pb2_pydantic import User
    # US/Eastern is UTC-5 (or UTC-4 with DST)
    eastern = timezone(timedelta(hours=-5))
    dt_eastern = datetime(2025, 6, 15, 7, 30, 45, 123000, tzinfo=eastern)
    u = User(email="a@b.com", display_name="X")
    serialized = u._serialize_datetime(dt_eastern, None)
    # Should convert to UTC: 7:30 EST = 12:30 UTC
    assert serialized == "2025-06-15T12:30:45.123Z", f"Got: {serialized}"


test("Datetime tz-aware non-UTC conversion to UTC", test_datetime_tz_conversion)


# ---------------------------------------------------------------------------
# Test: Bytes base64 serialization
# ---------------------------------------------------------------------------
def test_bytes_serialization():
    from user_pb2_pydantic import User
    u = User(email="a@b.com", display_name="X", avatar=b"\xCA\xFE")
    serialized = u._serialize_bytes(b"\xCA\xFE", None)
    assert serialized == "yv4=", f"Got: {serialized}"


test("Bytes base64 serialization", test_bytes_serialization)


# ---------------------------------------------------------------------------
# Test: Python keyword escaping
# ---------------------------------------------------------------------------
def test_keyword_escaping():
    from keywords_pb2_pydantic import KeywordFields
    # 'type' is a Python keyword, escaped to 'type_' with alias='type'.
    # Via alias (always works):
    k = KeywordFields(type="test_type")
    assert k.type_ == "test_type", f"Expected 'test_type', got {k.type_!r}"


test("Python keyword escaping (alias)", test_keyword_escaping)


# ---------------------------------------------------------------------------
# Test: populate_by_name — field name also works
# ---------------------------------------------------------------------------
def test_populate_by_name():
    from keywords_pb2_pydantic import KeywordFields
    # populate_by_name=True means we can use the field name too.
    k = KeywordFields(type_="by_field_name")
    assert k.type_ == "by_field_name", f"Expected 'by_field_name', got {k.type_!r}"
    # Verify model_config is set correctly.
    assert KeywordFields.model_config.get("populate_by_name") is True


test("populate_by_name allows field name", test_populate_by_name)


# ---------------------------------------------------------------------------
# Test: Repeated/map fields default to empty (not None)
# ---------------------------------------------------------------------------
def test_repeated_map_defaults():
    from user_pb2_pydantic import User
    u = User(email="a@b.com", display_name="X")
    assert u.roles == [], f"Expected empty list, got {u.roles}"
    assert u.metadata == {}, f"Expected empty dict, got {u.metadata}"
    assert isinstance(u.roles, list)
    assert isinstance(u.metadata, dict)
    # Verify we can iterate without None checks
    for _ in u.roles:
        pass
    for _ in u.metadata:
        pass


test("Repeated/map fields default to empty collections", test_repeated_map_defaults)


# ---------------------------------------------------------------------------
# Test: Map with message values
# ---------------------------------------------------------------------------
def test_map_message_values():
    from complex_pb2_pydantic import Document, Settings, SettingsTheme
    s = Settings(theme=SettingsTheme.themelight, locale="en-US")
    doc = Document(id="doc-1", settings_map={"default": s})
    data = json.loads(doc.model_dump_json())
    assert data["settings_map"]["default"]["locale"] == "en-US"


test("Map with message values", test_map_message_values)


# ---------------------------------------------------------------------------
# Test: 'self' keyword field escaping
# ---------------------------------------------------------------------------
def test_self_keyword_field():
    from keywords_pb2_pydantic import KeywordFields
    # 'self' is escaped to 'self_' with alias='self'.
    # Via alias:
    k = KeywordFields(self=42)
    assert k.self_ == 42, f"Expected 42 via alias, got {k.self_!r}"
    # Via field name (populate_by_name=True):
    k2 = KeywordFields(self_=99)
    assert k2.self_ == 99, f"Expected 99 via field name, got {k2.self_!r}"


test("'self' keyword field escaping", test_self_keyword_field)


# ---------------------------------------------------------------------------
# Test: 'cls', 'match', 'super' keyword field escaping
# ---------------------------------------------------------------------------
def test_other_keyword_fields():
    from keywords_pb2_pydantic import KeywordFields
    k = KeywordFields(cls=True, match=True, super="s")
    assert k.cls_ is True, f"Expected True via alias, got {k.cls_!r}"
    assert k.match_ is True, f"Expected True via alias, got {k.match_!r}"
    assert k.super_ == "s", f"Expected 's' via alias, got {k.super_!r}"


test("cls/match/super keyword field escaping", test_other_keyword_fields)


# ---------------------------------------------------------------------------
# Test: Email constraint pattern rejects invalid emails
# ---------------------------------------------------------------------------
def test_email_constraint_pattern():
    from user_pb2_pydantic import User
    from pydantic import ValidationError
    # Valid email should pass
    u = User(email="user@example.com", display_name="X")
    assert u.email == "user@example.com"
    # Missing @ should fail
    try:
        User(email="not-an-email", display_name="X")
        raise AssertionError("Email without @ should fail pattern")
    except ValidationError:
        pass
    # Missing domain dot should fail
    try:
        User(email="user@nodot", display_name="X")
        raise AssertionError("Email without domain dot should fail pattern")
    except ValidationError:
        pass


test("Email constraint pattern rejects invalid emails", test_email_constraint_pattern)


# ---------------------------------------------------------------------------
# Test: WKT Value fields (single_value, values, value_map)
# ---------------------------------------------------------------------------
def test_wkt_value_fields():
    from user_pb2_pydantic import User
    u = User(
        email="wkt@example.com",
        display_name="WKT User",
        roles=["user"],
        single_value="hello",
        values=["a", 1, True, None, {"nested": "dict"}],
        value_map={"str_key": "val", "int_key": 42, "bool_key": False, "null_key": None},
    )
    assert u.single_value == "hello"
    assert u.values == ["a", 1, True, None, {"nested": "dict"}]
    assert u.value_map["null_key"] is None
    data = u.model_dump()
    restored = User.model_validate(data)
    assert restored.single_value == "hello"
    assert restored.values == ["a", 1, True, None, {"nested": "dict"}]
    assert restored.value_map["int_key"] == 42


test("WKT Value fields (single_value, values, value_map)", test_wkt_value_fields)


# ---------------------------------------------------------------------------
# Test: WKT wrapper map fields (labels, scores)
# ---------------------------------------------------------------------------
def test_wkt_wrapper_map_fields():
    from user_pb2_pydantic import User
    u = User(
        email="wkt@example.com",
        display_name="WKT User",
        roles=["user"],
        labels={"env": "prod", "region": None},
        scores={"math": 95, "science": None},
    )
    assert u.labels["env"] == "prod"
    assert u.labels["region"] is None
    assert u.scores["math"] == 95
    assert u.scores["science"] is None
    data = u.model_dump()
    restored = User.model_validate(data)
    assert restored.labels == {"env": "prod", "region": None}
    assert restored.scores == {"math": 95, "science": None}


test("WKT wrapper map fields (labels, scores)", test_wkt_wrapper_map_fields)


# ---------------------------------------------------------------------------
# Test: WKT Struct fields (extra_metadata, structs, configs)
# ---------------------------------------------------------------------------
def test_wkt_struct_fields():
    from user_pb2_pydantic import User
    u = User(
        email="wkt@example.com",
        display_name="WKT User",
        roles=["user"],
        extra_metadata={"level": 5, "tags": ["a", "b"]},
        structs=[{"x": 1}, {"y": "two"}],
        configs={"db": {"host": "localhost", "port": 5432}},
    )
    assert u.extra_metadata["level"] == 5
    assert u.structs[1]["y"] == "two"
    assert u.configs["db"]["port"] == 5432
    data = u.model_dump()
    restored = User.model_validate(data)
    assert restored.extra_metadata == {"level": 5, "tags": ["a", "b"]}
    assert restored.structs == [{"x": 1}, {"y": "two"}]
    assert restored.configs["db"]["host"] == "localhost"


test("WKT Struct fields (extra_metadata, structs, configs)", test_wkt_struct_fields)


# ---------------------------------------------------------------------------
# Test: WKT ListValue fields (preferences, lists)
# ---------------------------------------------------------------------------
def test_wkt_listvalue_fields():
    from user_pb2_pydantic import User
    u = User(
        email="wkt@example.com",
        display_name="WKT User",
        roles=["user"],
        preferences=["dark_mode", 42, True],
        lists=[["a", "b"], [1, 2, 3]],
    )
    assert u.preferences == ["dark_mode", 42, True]
    assert u.lists[0] == ["a", "b"]
    assert u.lists[1] == [1, 2, 3]
    data = u.model_dump()
    restored = User.model_validate(data)
    assert restored.preferences == ["dark_mode", 42, True]
    assert restored.lists == [["a", "b"], [1, 2, 3]]


test("WKT ListValue fields (preferences, lists)", test_wkt_listvalue_fields)


# ---------------------------------------------------------------------------
# Test: WKT FieldMask fields (update_mask, field_masks)
# ---------------------------------------------------------------------------
def test_wkt_fieldmask_fields():
    from user_pb2_pydantic import User
    u = User(
        email="wkt@example.com",
        display_name="WKT User",
        roles=["user"],
        update_mask=["email", "display_name", "active"],
        field_masks=[["a", "b"], ["c"]],
    )
    assert u.update_mask == ["email", "display_name", "active"]
    assert u.field_masks == [["a", "b"], ["c"]]
    data = u.model_dump()
    restored = User.model_validate(data)
    assert restored.update_mask == ["email", "display_name", "active"]
    assert restored.field_masks == [["a", "b"], ["c"]]


test("WKT FieldMask fields (update_mask, field_masks)", test_wkt_fieldmask_fields)


# ---------------------------------------------------------------------------
# Test: WKT Timestamp map (event_times)
# ---------------------------------------------------------------------------
def test_wkt_timestamp_map():
    from user_pb2_pydantic import User
    dt1 = datetime(2025, 1, 15, 10, 0, 0, tzinfo=timezone.utc)
    dt2 = datetime(2026, 7, 4, 18, 30, 0, tzinfo=timezone.utc)
    u = User(
        email="wkt@example.com",
        display_name="WKT User",
        roles=["user"],
        event_times={"login": dt1, "signup": dt2},
    )
    assert u.event_times["login"].year == 2025
    assert u.event_times["signup"].year == 2026
    data = u.model_dump()
    restored = User.model_validate(data)
    assert restored.event_times["login"].year == 2025
    assert restored.event_times["signup"].year == 2026


test("WKT Timestamp map (event_times)", test_wkt_timestamp_map)


# ---------------------------------------------------------------------------
# Test: WKT all fields JSON roundtrip
# ---------------------------------------------------------------------------
def test_wkt_all_fields_json_roundtrip():
    from user_pb2_pydantic import User
    dt = datetime(2025, 3, 20, 8, 0, 0, tzinfo=timezone.utc)
    u = User(
        email="wkt@example.com",
        display_name="WKT User",
        roles=["user"],
        single_value={"nested": True},
        values=[1, "two", None],
        value_map={"k": "v"},
        labels={"env": "staging", "empty": None},
        scores={"total": 100},
        extra_metadata={"debug": False},
        preferences=["fast", "secure"],
        structs=[{"s": 1}],
        lists=[[1, 2], ["a"]],
        configs={"cache": {"ttl": 60}},
        update_mask=["email"],
        field_masks=[["a", "b"]],
        event_times={"deploy": dt},
    )
    json_str = u.model_dump_json()
    restored = User.model_validate_json(json_str)
    assert restored.email == "wkt@example.com"
    assert restored.single_value == {"nested": True}
    assert restored.values == [1, "two", None]
    assert restored.labels["empty"] is None
    assert restored.scores["total"] == 100
    assert restored.extra_metadata == {"debug": False}
    assert restored.preferences == ["fast", "secure"]
    assert restored.update_mask == ["email"]
    assert restored.field_masks == [["a", "b"]]
    assert restored.configs["cache"]["ttl"] == 60
    assert restored.event_times["deploy"].year == 2025


test("WKT all fields JSON roundtrip", test_wkt_all_fields_json_roundtrip)


# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
print(f"\n1..{tests_run}")
print(f"# {tests_passed} passed, {tests_failed} failed")
sys.exit(1 if tests_failed > 0 else 0)
