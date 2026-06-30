// Integration tests for proto2type-generated Rust code with real SQLite.
//
// Validates:
//  - Domain ↔ Row round-trips (to_domain / from_domain / into_domain)
//  - TryFrom impls
//  - Enum from_i32, Display, Default
//  - Oneof Some/None round-trip
//  - Empty/default values
//  - SQLite persistence via rusqlite (in-memory)
//  - ConversionError Display
//  - Nested message Option<Box<T>> round-trip
//  - Catalog doc-id pattern (model_id excluded from Row)

use chrono::{TimeZone, Utc};
use rusqlite::Connection;
use std::collections::HashMap;
use std::convert::TryFrom;

// Re-export generated crate
use proto2type_rust_integration::catalog::sqlite::ModelCatalogEntryRow;
use proto2type_rust_integration::catalog::ModelCatalogEntry;
use proto2type_rust_integration::user::sqlite::{AddressRow, ConversionError, TagRow, UserRow};
use proto2type_rust_integration::user::{Address, Tag, User, UserContactMethod, UserStatus};

// ---------------------------------------------------------------------------
// Helpers – all domain structs are #[non_exhaustive], so we build via
// Default::default() + field assignment from outside the defining crate.
// ---------------------------------------------------------------------------

fn make_address(street: &str, city: &str, state: &str, zip: &str, country: &str) -> Address {
    let mut a = Address::default();
    a.street = street.into();
    a.city = city.into();
    a.state = state.into();
    a.zip = zip.into();
    a.country = country.into();
    a
}

fn make_tag(key: &str, value: &str) -> Tag {
    let mut t = Tag::default();
    t.key = key.into();
    t.value = value.into();
    t
}

fn sample_user() -> User {
    let mut u = User::default();
    u.id = "u-123".into();
    u.email = "alice@example.com".into();
    u.display_name = "Alice".into();
    u.active = true;
    u.age = 30;
    u.roles = vec!["admin".into(), "editor".into()];
    u.metadata = {
        let mut m = HashMap::new();
        m.insert("tier".into(), "premium".into());
        m
    };
    u.address = Some(Box::new(make_address(
        "123 Main St",
        "Springfield",
        "IL",
        "62701",
        "US",
    )));
    u.created_at = Utc.with_ymd_and_hms(2024, 1, 15, 10, 30, 0).unwrap();
    u.session_timeout = 3_600_000;
    u.phone = Some("+15551234567".into());
    u.avatar = vec![0xDE, 0xAD, 0xBE, 0xEF];
    u.nickname = Some("ally".into());
    u.status = UserStatus::Active;
    u.contact_method = Some(UserContactMethod::ContactEmail(
        "alice-alt@example.com".into(),
    ));
    u.tags = vec![make_tag("dept", "eng"), make_tag("level", "senior")];
    u
}

fn sample_catalog_entry() -> ModelCatalogEntry {
    let mut e = ModelCatalogEntry::default();
    e.model_id = "gpt-4o".into();
    e.provider = "openai".into();
    e.display_name = "GPT-4o".into();
    e.input_per_million = 5.0;
    e.output_per_million = 15.0;
    e.enabled = true;
    e.category = "chat".into();
    e.context_window = 128_000;
    e.discount_percent = 0.0;
    e.aliases = vec!["gpt4o".into(), "gpt-4-omni".into()];
    e.provider_model_id = "gpt-4o-2024-05-13".into();
    e.created_at = Utc.with_ymd_and_hms(2024, 5, 13, 0, 0, 0).unwrap();
    e.updated_at = Utc.with_ymd_and_hms(2024, 6, 1, 12, 0, 0).unwrap();
    e.notes = "Latest multimodal model".into();
    e.region = "us-east-1".into();
    e
}

// ---------------------------------------------------------------------------
// User: basic round-trip via to_domain / from_domain
// ---------------------------------------------------------------------------

#[test]
fn test_user_roundtrip_to_domain() {
    let user = sample_user();
    let row = UserRow::from_domain(&user).expect("from_domain");
    let back = row.to_domain().expect("to_domain");
    assert_eq!(user, back, "round-trip via to_domain must be lossless");
}

// ---------------------------------------------------------------------------
// User: into_domain consuming variant
// ---------------------------------------------------------------------------

#[test]
fn test_user_into_domain() {
    let user = sample_user();
    let row = UserRow::from_domain(&user).expect("from_domain");
    let back = row.into_domain().expect("into_domain");
    assert_eq!(user, back, "round-trip via into_domain must be lossless");
}

// ---------------------------------------------------------------------------
// User: TryFrom impls
// ---------------------------------------------------------------------------

#[test]
fn test_user_try_from_ref() {
    let user = sample_user();
    let row = UserRow::try_from(&user).expect("TryFrom<&User>");
    let back: User = User::try_from(row).expect("TryFrom<UserRow>");
    assert_eq!(user, back);
}

// ---------------------------------------------------------------------------
// Catalog: doc-id pattern – model_id is a parameter, not in Row
// ---------------------------------------------------------------------------

#[test]
fn test_catalog_roundtrip_to_domain() {
    let entry = sample_catalog_entry();
    let row = ModelCatalogEntryRow::from_domain(&entry).expect("from_domain");
    let back = row.to_domain("gpt-4o".into()).expect("to_domain");
    assert_eq!(entry, back);
}

#[test]
fn test_catalog_into_domain() {
    let entry = sample_catalog_entry();
    let row = ModelCatalogEntryRow::from_domain(&entry).expect("from_domain");
    let back = row.into_domain("gpt-4o".into()).expect("into_domain");
    assert_eq!(entry, back);
}

#[test]
fn test_catalog_try_from_ref() {
    let entry = sample_catalog_entry();
    let row = ModelCatalogEntryRow::try_from(&entry).expect("TryFrom<&ModelCatalogEntry>");
    // No TryFrom<ModelCatalogEntryRow> for ModelCatalogEntry (doc-id needs external key)
    let back = row.to_domain("gpt-4o".into()).expect("to_domain");
    assert_eq!(entry, back);
}

// ---------------------------------------------------------------------------
// Enum: from_i32
// ---------------------------------------------------------------------------

#[test]
fn test_user_status_from_i32() {
    assert_eq!(UserStatus::from_i32(0), Some(UserStatus::Unspecified));
    assert_eq!(UserStatus::from_i32(1), Some(UserStatus::Active));
    assert_eq!(UserStatus::from_i32(2), Some(UserStatus::Suspended));
    assert_eq!(UserStatus::from_i32(3), Some(UserStatus::Deleted));
    assert_eq!(UserStatus::from_i32(99), None);
    assert_eq!(UserStatus::from_i32(-1), None);
}

// ---------------------------------------------------------------------------
// Enum: Display
// ---------------------------------------------------------------------------

#[test]
fn test_user_status_display() {
    assert_eq!(UserStatus::Unspecified.to_string(), "USER_STATUS_UNSPECIFIED");
    assert_eq!(UserStatus::Active.to_string(), "USER_STATUS_ACTIVE");
    assert_eq!(UserStatus::Suspended.to_string(), "USER_STATUS_SUSPENDED");
    assert_eq!(UserStatus::Deleted.to_string(), "USER_STATUS_DELETED");
}

// ---------------------------------------------------------------------------
// Enum: Default
// ---------------------------------------------------------------------------

#[test]
fn test_user_status_default() {
    let d: UserStatus = Default::default();
    assert_eq!(d, UserStatus::Unspecified);
}

// ---------------------------------------------------------------------------
// Oneof: Some round-trip
// ---------------------------------------------------------------------------

#[test]
fn test_oneof_some_email() {
    let mut user = sample_user();
    user.contact_method = Some(UserContactMethod::ContactEmail("x@y.com".into()));
    let row = UserRow::from_domain(&user).expect("from_domain");
    let back = row.to_domain().expect("to_domain");
    assert_eq!(user.contact_method, back.contact_method);
}

#[test]
fn test_oneof_some_phone() {
    let mut user = sample_user();
    user.contact_method = Some(UserContactMethod::ContactPhone("+1234".into()));
    let row = UserRow::from_domain(&user).expect("from_domain");
    let back = row.to_domain().expect("to_domain");
    assert_eq!(user.contact_method, back.contact_method);
}

// ---------------------------------------------------------------------------
// Oneof: None round-trip
// ---------------------------------------------------------------------------

#[test]
fn test_oneof_none() {
    let mut user = sample_user();
    user.contact_method = None;
    let row = UserRow::from_domain(&user).expect("from_domain");
    assert!(row.contact_method.is_none());
    let back = row.to_domain().expect("to_domain");
    assert!(back.contact_method.is_none());
}

// ---------------------------------------------------------------------------
// Empty / default values round-trip
// ---------------------------------------------------------------------------

#[test]
fn test_empty_defaults() {
    let user = User::default();
    let row = UserRow::from_domain(&user).expect("from_domain default");
    let back = row.to_domain().expect("to_domain default");
    assert_eq!(back.id, "");
    assert_eq!(back.email, "");
    assert_eq!(back.display_name, "");
    assert!(!back.active);
    assert_eq!(back.age, 0);
    assert!(back.roles.is_empty());
    assert!(back.metadata.is_empty());
    assert!(back.address.is_none());
    assert_eq!(back.session_timeout, 0);
    assert!(back.phone.is_none());
    assert!(back.avatar.is_empty());
    assert!(back.nickname.is_none());
    assert_eq!(back.status, UserStatus::Unspecified);
    assert!(back.contact_method.is_none());
    assert!(back.tags.is_empty());
}

// ---------------------------------------------------------------------------
// Nested message Option<Box<T>> round-trip
// ---------------------------------------------------------------------------

#[test]
fn test_nested_address_some() {
    let user = sample_user();
    let row = UserRow::from_domain(&user).expect("from_domain");
    // Address serialised as JSON string
    assert!(row.address.is_some());
    let back = row.to_domain().expect("to_domain");
    assert_eq!(user.address, back.address);
}

#[test]
fn test_nested_address_none() {
    let mut user = sample_user();
    user.address = None;
    let row = UserRow::from_domain(&user).expect("from_domain");
    assert!(row.address.is_none());
    let back = row.to_domain().expect("to_domain");
    assert!(back.address.is_none());
}

// ---------------------------------------------------------------------------
// AddressRow / TagRow round-trips
// ---------------------------------------------------------------------------

#[test]
fn test_address_row_roundtrip() {
    let addr = make_address("1 Infinite Loop", "Cupertino", "CA", "95014", "US");
    let row = AddressRow::from_domain(&addr).expect("from_domain");
    let back = row.to_domain().expect("to_domain");
    assert_eq!(addr, back);
}

#[test]
fn test_address_try_from() {
    let addr = make_address("1 Infinite Loop", "Cupertino", "CA", "95014", "US");
    let row = AddressRow::try_from(&addr).expect("TryFrom<&Address>");
    let back = Address::try_from(row).expect("TryFrom<AddressRow>");
    assert_eq!(addr, back);
}

#[test]
fn test_tag_row_roundtrip() {
    let tag = make_tag("env", "prod");
    let row = TagRow::from_domain(&tag).expect("from_domain");
    let back = row.into_domain().expect("into_domain");
    assert_eq!(tag, back);
}

#[test]
fn test_tag_try_from() {
    let tag = make_tag("env", "prod");
    let row = TagRow::try_from(&tag).expect("TryFrom<&Tag>");
    let back = Tag::try_from(row).expect("TryFrom<TagRow>");
    assert_eq!(tag, back);
}

// ---------------------------------------------------------------------------
// ConversionError Display
// ---------------------------------------------------------------------------

#[test]
fn test_conversion_error_display_json() {
    let bad: Result<Vec<String>, _> = serde_json::from_str("not-json");
    let err = ConversionError::Json(bad.unwrap_err());
    let msg = format!("{err}");
    assert!(msg.starts_with("json: "), "unexpected: {msg}");
}

#[test]
fn test_conversion_error_display_timestamp() {
    let err = ConversionError::InvalidTimestamp(i64::MIN);
    let msg = format!("{err}");
    assert!(msg.contains("invalid timestamp"), "unexpected: {msg}");
}

// ---------------------------------------------------------------------------
// SQLite persistence (in-memory): User
// ---------------------------------------------------------------------------

#[test]
fn test_sqlite_user_roundtrip() {
    let conn = Connection::open_in_memory().expect("open in-memory");
    conn.execute_batch(
        "CREATE TABLE users (
            id            TEXT    NOT NULL,
            email         TEXT    NOT NULL,
            display_name  TEXT    NOT NULL,
            active        BOOLEAN NOT NULL,
            age           INTEGER NOT NULL,
            roles         TEXT    NOT NULL,
            metadata      TEXT    NOT NULL,
            address       TEXT,
            created_at    INTEGER NOT NULL,
            session_timeout INTEGER NOT NULL,
            phone         TEXT,
            avatar        BLOB    NOT NULL,
            nickname      TEXT,
            status        INTEGER NOT NULL,
            contact_method TEXT,
            tags          TEXT    NOT NULL,
            deleted_at    INTEGER,
            previous_status INTEGER
        );",
    )
    .expect("create table");

    let user = sample_user();
    let row = UserRow::from_domain(&user).expect("from_domain");

    conn.execute(
        "INSERT INTO users (
            id, email, display_name, active, age, roles, metadata,
            address, created_at, session_timeout, phone, avatar,
            nickname, status, contact_method, tags, deleted_at, previous_status
        ) VALUES (
            ?1, ?2, ?3, ?4, ?5, ?6, ?7,
            ?8, ?9, ?10, ?11, ?12,
            ?13, ?14, ?15, ?16, ?17, ?18
        )",
        rusqlite::params![
            row.id,
            row.email,
            row.display_name,
            row.active,
            row.age,
            row.roles,
            row.metadata,
            row.address,
            row.created_at,
            row.session_timeout,
            row.phone,
            row.avatar,
            row.nickname,
            row.status,
            row.contact_method,
            row.tags,
            row.deleted_at,
            row.previous_status,
        ],
    )
    .expect("insert");

    let queried = conn
        .query_row("SELECT * FROM users WHERE id = ?1", [&user.id], |row| {
            UserRow::from_row(row)
        })
        .expect("query");

    let back = queried.to_domain().expect("to_domain from sqlite");
    assert_eq!(user, back, "SQLite round-trip must be lossless");
}

// ---------------------------------------------------------------------------
// SQLite persistence (in-memory): Catalog with doc-id
// ---------------------------------------------------------------------------

#[test]
fn test_sqlite_catalog_roundtrip() {
    let conn = Connection::open_in_memory().expect("open in-memory");
    conn.execute_batch(
        "CREATE TABLE catalog (
            model_id          TEXT    NOT NULL PRIMARY KEY,
            provider          TEXT    NOT NULL,
            display_name      TEXT    NOT NULL,
            input_per_million REAL    NOT NULL,
            output_per_million REAL   NOT NULL,
            enabled           BOOLEAN NOT NULL,
            category          TEXT    NOT NULL,
            context_window    INTEGER NOT NULL,
            discount_percent  REAL    NOT NULL,
            aliases           TEXT    NOT NULL,
            provider_model_id TEXT    NOT NULL,
            created_at        INTEGER NOT NULL,
            updated_at        INTEGER NOT NULL,
            notes             TEXT    NOT NULL,
            region            TEXT    NOT NULL
        );",
    )
    .expect("create table");

    let entry = sample_catalog_entry();
    let row = ModelCatalogEntryRow::from_domain(&entry).expect("from_domain");

    // model_id is the doc-id, stored separately from the row
    conn.execute(
        "INSERT INTO catalog (
            model_id, provider, display_name, input_per_million, output_per_million,
            enabled, category, context_window, discount_percent, aliases,
            provider_model_id, created_at, updated_at, notes, region
        ) VALUES (
            ?1, ?2, ?3, ?4, ?5,
            ?6, ?7, ?8, ?9, ?10,
            ?11, ?12, ?13, ?14, ?15
        )",
        rusqlite::params![
            entry.model_id,
            row.provider,
            row.display_name,
            row.input_per_million,
            row.output_per_million,
            row.enabled,
            row.category,
            row.context_window,
            row.discount_percent,
            row.aliases,
            row.provider_model_id,
            row.created_at,
            row.updated_at,
            row.notes,
            row.region,
        ],
    )
    .expect("insert");

    let (doc_id, queried) = conn
        .query_row(
            "SELECT * FROM catalog WHERE model_id = ?1",
            [&entry.model_id],
            |row| {
                let mid: String = row.get("model_id")?;
                let r = ModelCatalogEntryRow::from_row(row)?;
                Ok((mid, r))
            },
        )
        .expect("query");

    let back = queried.to_domain(doc_id).expect("to_domain from sqlite");
    assert_eq!(entry, back, "SQLite catalog round-trip must be lossless");
}

// ---------------------------------------------------------------------------
// SQLite: multiple rows
// ---------------------------------------------------------------------------

#[test]
fn test_sqlite_multiple_users() {
    let conn = Connection::open_in_memory().expect("open in-memory");
    conn.execute_batch(
        "CREATE TABLE users (
            id TEXT NOT NULL, email TEXT NOT NULL, display_name TEXT NOT NULL,
            active BOOLEAN NOT NULL, age INTEGER NOT NULL, roles TEXT NOT NULL,
            metadata TEXT NOT NULL, address TEXT, created_at INTEGER NOT NULL,
            session_timeout INTEGER NOT NULL, phone TEXT, avatar BLOB NOT NULL,
            nickname TEXT, status INTEGER NOT NULL, contact_method TEXT, tags TEXT NOT NULL,
            deleted_at INTEGER, previous_status INTEGER
        );",
    )
    .expect("create table");

    let mut users = Vec::new();
    for i in 0..5 {
        let mut u = User::default();
        u.id = format!("user-{i}");
        u.email = format!("user{i}@test.com");
        u.display_name = format!("User {i}");
        u.status = UserStatus::from_i32(i % 4).unwrap_or_default();
        users.push(u);
    }

    for user in &users {
        let row = UserRow::from_domain(user).expect("from_domain");
        conn.execute(
            "INSERT INTO users VALUES (?1,?2,?3,?4,?5,?6,?7,?8,?9,?10,?11,?12,?13,?14,?15,?16,?17,?18)",
            rusqlite::params![
                row.id, row.email, row.display_name, row.active, row.age, row.roles,
                row.metadata, row.address, row.created_at, row.session_timeout,
                row.phone, row.avatar, row.nickname, row.status, row.contact_method, row.tags,
                row.deleted_at, row.previous_status,
            ],
        )
        .expect("insert");
    }

    let mut stmt = conn.prepare("SELECT * FROM users ORDER BY id").expect("prepare");
    let rows: Vec<UserRow> = stmt
        .query_map([], |row| UserRow::from_row(row))
        .expect("query_map")
        .collect::<Result<_, _>>()
        .expect("collect");

    assert_eq!(rows.len(), 5);
    for (i, row) in rows.iter().enumerate() {
        let domain = row.to_domain().expect("to_domain");
        assert_eq!(domain, users[i]);
    }
}

// ---------------------------------------------------------------------------
// Edge case: very large metadata
// ---------------------------------------------------------------------------

#[test]
fn test_large_metadata_roundtrip() {
    let mut user = User::default();
    for i in 0..100 {
        user.metadata
            .insert(format!("key_{i}"), format!("value_{i}"));
    }
    let row = UserRow::from_domain(&user).expect("from_domain");
    let back = row.to_domain().expect("to_domain");
    assert_eq!(user.metadata.len(), back.metadata.len());
    assert_eq!(user, back);
}

// ---------------------------------------------------------------------------
// Edge case: unicode in strings
// ---------------------------------------------------------------------------

#[test]
fn test_unicode_roundtrip() {
    let mut user = User::default();
    user.display_name = "日本語テスト 🎉".into();
    user.email = "用户@例え.jp".into();
    user.address = Some(Box::new(make_address(
        "東京都渋谷区",
        "東京",
        "東京都",
        "150-0002",
        "日本",
    )));
    let row = UserRow::from_domain(&user).expect("from_domain");
    let back = row.to_domain().expect("to_domain");
    assert_eq!(user, back);
}

// ---------------------------------------------------------------------------
// Edge case: binary avatar through SQLite
// ---------------------------------------------------------------------------

#[test]
fn test_binary_blob_sqlite() {
    let conn = Connection::open_in_memory().expect("open in-memory");
    conn.execute_batch(
        "CREATE TABLE users (
            id TEXT NOT NULL, email TEXT NOT NULL, display_name TEXT NOT NULL,
            active BOOLEAN NOT NULL, age INTEGER NOT NULL, roles TEXT NOT NULL,
            metadata TEXT NOT NULL, address TEXT, created_at INTEGER NOT NULL,
            session_timeout INTEGER NOT NULL, phone TEXT, avatar BLOB NOT NULL,
            nickname TEXT, status INTEGER NOT NULL, contact_method TEXT, tags TEXT NOT NULL,
            deleted_at INTEGER, previous_status INTEGER
        );",
    )
    .expect("create");

    let mut user = User::default();
    // All 256 byte values
    user.avatar = (0u8..=255).collect();
    user.id = "blob-test".into();

    let row = UserRow::from_domain(&user).expect("from_domain");
    conn.execute(
        "INSERT INTO users VALUES (?1,?2,?3,?4,?5,?6,?7,?8,?9,?10,?11,?12,?13,?14,?15,?16,?17,?18)",
        rusqlite::params![
            row.id, row.email, row.display_name, row.active, row.age, row.roles,
            row.metadata, row.address, row.created_at, row.session_timeout,
            row.phone, row.avatar, row.nickname, row.status, row.contact_method, row.tags,
            row.deleted_at, row.previous_status,
        ],
    )
    .expect("insert");

    let queried = conn
        .query_row("SELECT * FROM users WHERE id = ?1", ["blob-test"], |r| {
            UserRow::from_row(r)
        })
        .expect("query");

    let back = queried.to_domain().expect("to_domain");
    assert_eq!(user.avatar, back.avatar);
    assert_eq!(user.avatar.len(), 256);
}
