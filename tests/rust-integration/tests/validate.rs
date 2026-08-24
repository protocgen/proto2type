use chrono::Utc;
use proto2type_rust_integration::user::{Address, User};
use validator::Validate;

fn valid_user() -> User {
    let mut u = User::default();
    u.id = "123".to_string();
    u.email = "test@example.com".to_string();
    u.display_name = "Test User".to_string();
    u.active = true;
    u.age = 30;
    u.roles = vec!["admin".to_string()];
    u.phone = Some("+1-555-1234".to_string());
    u.created_at = Utc::now();
    u.address = Some({
        let mut a = Address::default();
        a.street = "123 Main St".to_string();
        a.city = "San Francisco".to_string();
        a.state = "CA".to_string();
        a.zip = "94105".to_string();
        a.country = "USA".to_string();
        a
    });
    u
}

#[test]
fn test_user_validation_success() {
    let user = valid_user();
    assert!(user.validate().is_ok(), "valid user should pass: {:?}", user.validate());
}

#[test]
fn test_user_validation_email_failure() {
    let mut user = valid_user();
    user.email = "invalid-email".to_string();
    let res = user.validate();
    assert!(res.is_err());
    assert!(res.unwrap_err().field_errors().contains_key("email"));
}

#[test]
fn test_user_validation_display_name_too_short() {
    let mut user = valid_user();
    user.display_name = "".to_string();
    let res = user.validate();
    assert!(res.is_err());
    assert!(res.unwrap_err().field_errors().contains_key("display_name"));
}

#[test]
fn test_user_validation_display_name_too_long() {
    let mut user = valid_user();
    user.display_name = "a".repeat(256);
    let res = user.validate();
    assert!(res.is_err());
    assert!(res.unwrap_err().field_errors().contains_key("display_name"));
}

#[test]
fn test_user_validation_age_too_low() {
    let mut user = valid_user();
    user.age = -1;
    let res = user.validate();
    assert!(res.unwrap_err().field_errors().contains_key("age"));
}

#[test]
fn test_user_validation_age_too_high() {
    let mut user = valid_user();
    user.age = 151;
    let res = user.validate();
    assert!(res.unwrap_err().field_errors().contains_key("age"));
}

#[test]
fn test_user_validation_phone_invalid_pattern() {
    let mut user = valid_user();
    user.phone = Some("invalid phone!".to_string());
    let res = user.validate();
    assert!(res.unwrap_err().field_errors().contains_key("phone"));
}

#[test]
fn test_user_validation_phone_valid_pattern() {
    let mut user = valid_user();
    user.phone = Some("+123456789".to_string());
    assert!(user.validate().is_ok());
}

#[test]
fn test_user_validation_roles_too_few() {
    let mut user = valid_user();
    user.roles = vec![];
    let res = user.validate();
    assert!(res.unwrap_err().field_errors().contains_key("roles"));
}

#[test]
fn test_user_validation_roles_too_many() {
    let mut user = valid_user();
    user.roles = vec!["role".to_string(); 11];
    let res = user.validate();
    assert!(res.unwrap_err().field_errors().contains_key("roles"));
}

#[test]
fn test_address_nested_validation() {
    let mut user = valid_user();
    user.address = Some({
        let mut a = Address::default();
        a.street = "123 Main St".to_string();
        a.city = "City".to_string();
        a.state = "C".to_string(); // Too short (min_len: 2)
        a.zip = "94105".to_string();
        a
    });
    let res = user.validate();
    assert!(res.is_err());
    assert!(res.unwrap_err().errors().contains_key("address"));
}

#[test]
fn test_address_zip_pattern() {
    let mut a = Address::default();
    a.street = "123 Main St".to_string();
    a.city = "SF".to_string();
    a.state = "CA".to_string();
    a.zip = "BADZIP".to_string();
    a.country = "US".to_string();
    let res = a.validate();
    assert!(res.is_err());
    assert!(res.unwrap_err().field_errors().contains_key("zip"));
}

#[test]
fn test_address_valid() {
    let mut a = Address::default();
    a.street = "123 Main St".to_string();
    a.city = "SF".to_string();
    a.state = "CA".to_string();
    a.zip = "94105".to_string();
    a.country = "US".to_string();
    assert!(a.validate().is_ok());
}
