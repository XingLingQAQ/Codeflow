#!/bin/bash
# CodeFlow Build Script (Linux/macOS)
# Builds frontend and embeds it into Go backend

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"
OUTPUT_DIR="${1:-./dist}"
SKIP_FRONTEND="${2:-false}"

sync_frontend_dist() {
    local source_dir="$1"
    local target_dir="$2"

    if [ ! -d "$source_dir" ]; then
        echo "Error: Frontend dist not found: $source_dir"
        exit 1
    fi

    rm -rf "$target_dir"
    mkdir -p "$target_dir"
    cp -R "$source_dir"/. "$target_dir"/
}

install_frontend_deps() {
    local root_dir="$1"
    local frontend_dir="$2"

    # Prefer monorepo pnpm install at repo root for apps/desktop
    if [ -f "$root_dir/pnpm-workspace.yaml" ] && [ -f "$root_dir/package.json" ]; then
        if [ ! -d "$root_dir/node_modules" ]; then
            echo "Installing monorepo dependencies via pnpm..."
            (cd "$root_dir" && pnpm install)
        fi
        return 0
    fi

    if [ ! -d "$frontend_dir/node_modules" ]; then
        (
            cd "$frontend_dir"
            if [ -f package-lock.json ]; then
                npm ci
            else
                npm install
            fi
        )
    fi
}

build_frontend() {
    local root_dir="$1"
    local frontend_dir="$2"

    if [ -f "$root_dir/pnpm-workspace.yaml" ]; then
        echo "Building frontend with pnpm --filter @codeflow/desktop..."
        (cd "$root_dir" && pnpm --filter @codeflow/desktop build)
        return 0
    fi

    echo "Building frontend with Vite..."
    (cd "$frontend_dir" && npm run build)
}

echo "=== CodeFlow Build Script ==="
echo "Root directory: $ROOT_DIR"

# Step 1: Build Frontend
if [ "$SKIP_FRONTEND" != "true" ]; then
    echo -e "\n[1/3] Building frontend..."

    FRONTEND_DIR="$ROOT_DIR/apps/desktop"
    if [ ! -d "$FRONTEND_DIR" ]; then
        echo "Error: Frontend directory not found: $FRONTEND_DIR"
        exit 1
    fi

    echo "Installing frontend dependencies if needed..."
    install_frontend_deps "$ROOT_DIR" "$FRONTEND_DIR"
    build_frontend "$ROOT_DIR" "$FRONTEND_DIR"

    FRONTEND_DIST_DIR="$FRONTEND_DIR/dist"
    EMBEDDED_DIST_DIR="$ROOT_DIR/backend/internal/web/dist"
    echo "Syncing frontend dist to embedded backend assets..."
    sync_frontend_dist "$FRONTEND_DIST_DIR" "$EMBEDDED_DIST_DIR"

    echo "Frontend build complete!"
else
    echo -e "\n[1/3] Skipping frontend build"
fi

# Step 2: Build Go Backend
echo -e "\n[2/3] Building Go backend..."

BACKEND_DIR="$ROOT_DIR/backend"
cd "$BACKEND_DIR"

# Verify dist directory exists
DIST_DIR="$BACKEND_DIR/internal/web/dist"
if [ ! -d "$DIST_DIR" ]; then
    echo "Error: Frontend dist not found. Run without skip-frontend first."
    exit 1
fi

# Create output directory
OUTPUT_PATH="$ROOT_DIR/$OUTPUT_DIR"
mkdir -p "$OUTPUT_PATH"

# Detect OS and build
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$ARCH" in
    x86_64) GOARCH="amd64" ;;
    arm64|aarch64) GOARCH="arm64" ;;
    *) GOARCH="amd64" ;;
esac

case "$OS" in
    darwin) GOOS="darwin"; EXT="" ;;
    linux) GOOS="linux"; EXT="" ;;
    mingw*|msys*|cygwin*) GOOS="windows"; EXT=".exe" ;;
    *) GOOS="linux"; EXT="" ;;
esac

OUTPUT_FILE="$OUTPUT_PATH/codeflow-${GOOS}-${GOARCH}${EXT}"
echo "Building: $OUTPUT_FILE"

CGO_ENABLED=1 GOOS=$GOOS GOARCH=$GOARCH go build -ldflags="-s -w" -o "$OUTPUT_FILE" ./cmd/codeflow-server

echo "Backend build complete!"

# Step 3: Summary
echo -e "\n[3/3] Build Summary"
SIZE=$(du -h "$OUTPUT_FILE" | cut -f1)
echo "Output: $OUTPUT_FILE"
echo "Size: $SIZE"

echo -e "\n=== Build Complete ==="
echo "Run with: $OUTPUT_FILE"
echo "Access at: http://localhost:8080"