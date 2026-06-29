#!/usr/bin/env bash
# refresh.sh — copies golden files into src/ with proper module structure
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
GOLDEN="$REPO_ROOT/testdata/golden/rust/gen"

SRC="$SCRIPT_DIR/src"

# Clean previous generated source
rm -rf "$SRC"

# --- lib.rs ---
mkdir -p "$SRC"
cat > "$SRC/lib.rs" <<'EOF'
pub mod user;
pub mod catalog;
EOF

# --- user module ---
mkdir -p "$SRC/user"
cp "$GOLDEN/user.type.rs" "$SRC/user/mod.rs"
# Patch: UserStatus needs Serialize/Deserialize because User (which derives them)
# has a `status: UserStatus` field. This is a codegen gap we surface here.
sed -i.bak 's/#\[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Default)\]/#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Default, Serialize, Deserialize)]/' "$SRC/user/mod.rs"
rm -f "$SRC/user/mod.rs.bak"
echo '' >> "$SRC/user/mod.rs"
echo 'pub mod sqlite;' >> "$SRC/user/mod.rs"
cp "$GOLDEN/user_sqlite.type.rs" "$SRC/user/sqlite.rs"

# --- catalog module ---
mkdir -p "$SRC/catalog"
cp "$GOLDEN/catalog.type.rs" "$SRC/catalog/mod.rs"
echo '' >> "$SRC/catalog/mod.rs"
echo 'pub mod sqlite;' >> "$SRC/catalog/mod.rs"
cp "$GOLDEN/catalog_sqlite.type.rs" "$SRC/catalog/sqlite.rs"

echo "✓ Refreshed src/ from golden files"
