#!/bin/bash
# Builds Whispr.app — free distribution, ad-hoc signed, no Apple account.
# Models are NOT bundled: the app downloads them to Application Support on
# first run. The bundle carries the binary + the two native runtimes (~50MB).
#
# Prereqs: third_party/sherpa/lib and third_party/llama populated (README).
set -euo pipefail
cd "$(dirname "$0")/.."

APP=build/Whispr.app
VERSION="${1:-0.1.0}"

rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Frameworks" "$APP/Contents/Resources/llama"

echo "building binary..."
go build -o "$APP/Contents/MacOS/whispr" ./cmd/app

echo "bundling runtimes..."
cp third_party/sherpa/lib/*.dylib "$APP/Contents/Frameworks/"
cp third_party/llama/llama-server third_party/llama/*.dylib "$APP/Contents/Resources/llama/"

# The dev build's rpath points into the repo; inside the bundle the sherpa
# dylibs live in Frameworks. llama-server needs nothing: its rpath is
# @loader_path and its dylibs sit next to it.
install_name_tool -add_rpath "@executable_path/../Frameworks" "$APP/Contents/MacOS/whispr"

cat > "$APP/Contents/Info.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleName</key>            <string>Whispr</string>
	<key>CFBundleDisplayName</key>     <string>Whispr</string>
	<key>CFBundleIdentifier</key>      <string>com.adityapainuli.whispr</string>
	<key>CFBundleExecutable</key>      <string>whispr</string>
	<key>CFBundlePackageType</key>     <string>APPL</string>
	<key>CFBundleShortVersionString</key> <string>${VERSION}</string>
	<key>CFBundleVersion</key>         <string>${VERSION}</string>
	<key>LSMinimumSystemVersion</key>  <string>12.0</string>
	<key>NSHighResolutionCapable</key> <true/>
	<!-- menu bar only, no Dock icon -->
	<key>LSUIElement</key>             <true/>
	<key>NSMicrophoneUsageDescription</key>
	<string>Whispr listens to your microphone only while you dictate, and never sends audio anywhere.</string>
</dict>
</plist>
EOF

echo "signing (ad-hoc)..."
codesign --force --deep -s - "$APP"

echo "done: $APP ($(du -sh "$APP" | cut -f1))"
echo "zip for distribution: ditto -c -k --keepParent $APP build/Whispr-${VERSION}.zip"
