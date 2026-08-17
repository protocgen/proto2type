import { UserSchema } from '../gen/user.type.js';
import { UserSchema as UserValidateSchema } from '../gen/user_validate.type.js';

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

console.log(`\n1..${total}`);
console.log(`# ${passed} passed, ${failed} failed`);
process.exit(failed > 0 ? 1 : 0);
