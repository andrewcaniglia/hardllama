#!/bin/sh

# Builds dist/HardLlama.app for Apple Silicon. Only the app itself is compiled.
# The ollama server and its libraries come from an official release, so this
# needs Go and Node but not Xcode or cmake. It does not touch the installed app.
#
# Usage:
#   scripts/build_hardllama.sh            keep the installed server
#   scripts/build_hardllama.sh 0.41.0     take the server from that release

set -eu

OLLAMA_TEAM_ID=3MU9H2V9Y9
INSTALLED=/Applications/HardLlama.app
OUT=dist/HardLlama.app
SERVER_VERSION=${1:-}

status() { echo >&2 ">>> $*"; }
die() { echo >&2 "error: $*"; exit 1; }

[ "$(uname -sm)" = "Darwin arm64" ] || die "this script only builds for Apple Silicon Macs"
[ -f scripts/build_hardllama.sh ] || die "run this from the repository root"
command -v go >/dev/null || die "Go is not installed, try: brew install go"
command -v npm >/dev/null || die "Node is not installed, try: brew install node"

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

if [ -n "$SERVER_VERSION" ]; then
    status "Downloading Ollama $SERVER_VERSION"
    curl -fL --progress-bar -o "$WORK/Ollama-darwin.zip" \
        "https://github.com/ollama/ollama/releases/download/v$SERVER_VERSION/Ollama-darwin.zip"
    ditto -x -k "$WORK/Ollama-darwin.zip" "$WORK/release"
    BASE="$WORK/release/Ollama.app"

    status "Verifying the release signature"
    codesign --verify --deep --strict "$BASE" || die "release signature is invalid"
    TEAM=$(codesign -dv "$BASE" 2>&1 | sed -n 's/^TeamIdentifier=//p')
    [ "$TEAM" = "$OLLAMA_TEAM_ID" ] || die "release is signed by '$TEAM', not Ollama"
else
    [ -d "$INSTALLED" ] || die "$INSTALLED not found, pass a release version instead"
    BASE=$INSTALLED
    SERVER_VERSION=$(plutil -extract CFBundleShortVersionString raw "$BASE/Contents/Info.plist")
    SERVER_VERSION=${SERVER_VERSION%-hardllama}
fi
VERSION="$SERVER_VERSION-hardllama"

status "Building the UI"
(cd app/ui/app && npm ci --ignore-scripts --no-audit --no-fund && npm run build)

status "Building the app"
CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 \
    CGO_CFLAGS="-O3 -mmacosx-version-min=14.0" \
    CGO_CXXFLAGS="-O3 -mmacosx-version-min=14.0" \
    CGO_LDFLAGS="-mmacosx-version-min=14.0" \
    go build -o "$WORK/app" \
    -ldflags="-s -w -X=github.com/ollama/ollama/app/version.Version=$VERSION" ./app/cmd/app

status "Assembling $OUT"
rm -rf "$OUT"
mkdir -p dist
ditto "$BASE" "$OUT"
cp "$WORK/app" "$OUT/Contents/MacOS/Ollama"
cp "$WORK/app" "$OUT/Contents/Frameworks/Squirrel.framework/Versions/A/Squirrel"
for KEY in CFBundleName CFBundleDisplayName; do
    plutil -replace "$KEY" -string HardLlama "$OUT/Contents/Info.plist"
done
for KEY in CFBundleShortVersionString CFBundleVersion; do
    plutil -replace "$KEY" -string "$VERSION" "$OUT/Contents/Info.plist"
done

# Nested code keeps its official signature. The ad-hoc signature only covers
# the app, so the bundle identifier must match what instance detection expects.
codesign -f --deep -s - --identifier com.electron.ollama "$OUT"
codesign --verify --deep --strict "$OUT"

status "Built $OUT ($VERSION)"
cat >&2 <<INSTALL

To install it, quit HardLlama and run:
  rm -rf $INSTALLED && ditto $OUT $INSTALLED
INSTALL
