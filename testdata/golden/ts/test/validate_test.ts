import { UserSchema, CategorySchema } from '../gen/user.type.js';
import { UserSchema as UserValidateSchema } from '../gen/user_validate.type.js';
import { UserSchema as UserBigIntSchema } from '../gen/user_bigint.type.js';
// Note: Requires zod ^3.23.0 for .base64() support

// Helper to count pass/fail
let passed = 0;
let failed = 0;
let total = 0;

function assert(name: string, fn: () => boolean) {
  total++;
  try {
    if (fn()) {
      passed++;
      console.log(`ok ${total} - ${name}`);
    } else {
      failed++;
      console.log(`not ok ${total} - ${name}`);
    }
  } catch (e) {
    failed++;
    console.log(`not ok ${total} - ${name}: ${e}`);
  }
}

// Test 1: Valid user parses successfully
assert('valid user parses', () => {
  const result = UserSchema.safeParse({
    id: 'user-1',
    displayName: 'Test User',
    email: 'test@example.com',
    status: 'Active',
  });
  if (!result.success) console.error(result.error);
  return result.success;
});

// Test 2: Default values are applied
assert('default values applied', () => {
  const result = UserSchema.safeParse({});
  if (!result.success) return false;
  return result.data.displayName === '' && result.data.roles !== undefined;
});

// Test 3: Invalid data is rejected
assert('invalid data rejected by validate schema', () => {
  const result = UserValidateSchema.safeParse({
    email: 'not-an-email',
  });
  return !result.success;
});

// Test 4: Map fields default to empty
assert('map fields default to empty object', () => {
  const result = UserSchema.safeParse({});
  if (!result.success) return false;
  return typeof result.data.metadata === 'object';
});

// Test 5: Array fields default to empty
assert('array fields default to empty array', () => {
  const result = UserSchema.safeParse({});
  if (!result.success) return false;
  return Array.isArray(result.data.roles);
});

// Test 6: Enum has default value
assert('enum has default value', () => {
  const result = UserSchema.safeParse({});
  if (!result.success) return false;
  // Status should have a default string value, not be undefined
  return result.data.status !== undefined;
});

// Test: Oneof mutual exclusion runtime test
assert('oneof mutual exclusion', () => {
  const result = UserSchema.safeParse({
    contactEmail: 'a@a.com',
    contactPhone: '1234567890',
  });
  return !result.success; // should fail
});

// Age boundary: -1 (fail), 0 (pass), 150 (pass), 151 (fail)
assert('age boundary', () => {
  const base = { email: 'test@example.com', displayName: 'Test', roles: ['user'] };
  const r1 = UserValidateSchema.safeParse({ ...base, age: -1 });
  const r2 = UserValidateSchema.safeParse({ ...base, age: 0 });
  const r3 = UserValidateSchema.safeParse({ ...base, age: 150 });
  const r4 = UserValidateSchema.safeParse({ ...base, age: 151 });
  if (r1.success) console.error('  FAIL: age=-1 should fail but passed');
  if (!r2.success) console.error('  FAIL: age=0 should pass but failed:', JSON.stringify(r2.error.issues.map(i => ({p: i.path, m: i.message}))));
  if (!r3.success) console.error('  FAIL: age=150 should pass but failed:', JSON.stringify(r3.error.issues.map(i => ({p: i.path, m: i.message}))));
  if (r4.success) console.error('  FAIL: age=151 should fail but passed');
  return !r1.success && r2.success && r3.success && !r4.success;
});

// Empty string for required fields (should fail if min_len=1)
assert('empty string for required field', () => {
  const result = UserValidateSchema.safeParse({
    displayName: '',
  });
  return !result.success;
});

// Prototype pollution: test a map field with own-enumerable __proto__ key
assert('prototype pollution rejected', () => {
  const poisoned = Object.create(null);
  poisoned['__proto__'] = { hacked: true };
  const result = UserSchema.safeParse({
    metadata: poisoned
  });
  return !result.success;
});

// BigInt boundary: test with string "123", number 123, and unsafe integer "9007199254740993"
assert('bigint boundary', () => {
  const r1 = UserBigIntSchema.safeParse({ bigNumber: "123" });
  const r2 = UserBigIntSchema.safeParse({ bigNumber: 123 });
  const r3 = UserBigIntSchema.safeParse({ bigNumber: "9007199254740993" });
  return r1.success && r2.success && r3.success;
});

// Recursive type: parse a nested Category with children
assert('recursive type parse', () => {
  const result = CategorySchema.safeParse({
    name: 'root',
    children: [
      { name: 'child', children: [] }
    ]
  });
  return result.success;
});

console.log(`\n1..${total}`);
console.log(`# ${passed} passed, ${failed} failed`);
process.exit(failed > 0 ? 1 : 0);
