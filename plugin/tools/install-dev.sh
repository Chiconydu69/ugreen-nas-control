#!/bin/bash
# Installs the working copy on the NAS, without a release.
# Needs .build/bin/ugreen-nas: the multicall binary (`just go-build`, the CI artifact or a release binary).

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
DEST="/usr/local/emhttp/plugins/ugreen-nas-control"

if [ ! -d /usr/local/emhttp/plugins ]; then
    echo "/usr/local/emhttp/plugins missing – run this on the Unraid NAS." >&2
    exit 1
fi

echo "Installing UGREEN NAS Control development build..."
echo "Source: $ROOT"
echo "Target: $DEST"

if [ ! -f "$ROOT/.build/bin/ugreen-nas" ]; then
    echo ".build/bin/ugreen-nas missing – run \`just go-build\` or download the CI artifact first." >&2
    exit 1
fi

if [ ! -f "$ROOT/.build/plugin/js/app.js" ] || [ ! -f "$ROOT/.build/plugin/images/ugreen-nas-control.png" ]; then
    echo "Install tree .build/plugin missing – run \`just web-build\` first." >&2
    exit 1
fi

# The running daemon belongs to the old files; stopping it restores the BIOS fan values.
if [ -x "$DEST/backend/ugreen-nas-ctl" ]; then
    "$DEST/backend/ugreen-nas-ctl" led-stop >/dev/null 2>&1 || true
    "$DEST/backend/ugreen-nas-ctl" stop >/dev/null 2>&1 || true
fi

# Copy into a staging dir first; the old install is only replaced once every copy worked.
STAGE="$DEST.new"
rm -rf "$STAGE"
mkdir -p "$STAGE"

# .build/plugin is the install tree made by `just web-build` (plugin/files.txt: paths and modes); only the binaries come on top.
cp -R "$ROOT/.build/plugin/." "$STAGE/"
# One binary, the tool names are links to it (plugin/files.txt).
cp "$ROOT/.build/bin/ugreen-nas" "$STAGE/backend/"
chmod 755 "$STAGE/backend/ugreen-nas"
ln -s ugreen-nas "$STAGE/backend/ugreen-nas-ctl"
ln -s ugreen-nas "$STAGE/backend/ugreen-nas-fan"
mkdir -p "$STAGE/led/backend"
ln -s ../../backend/ugreen-nas "$STAGE/led/backend/ugreen-led-ctl"
ln -s ../../backend/ugreen-nas "$STAGE/led/backend/ugreen-led-i2c"

rm -rf "$DEST"
mv "$STAGE" "$DEST"

# The copy replaced the .page files, and with them a favorite's Menu label.
if [ -x /usr/local/emhttp/webGui/scripts/restore_favorites ]; then
    /usr/local/emhttp/webGui/scripts/restore_favorites
fi

# Like the .plg: without a supported fan controller, only system info and temperatures work.
if "$DEST/backend/ugreen-nas-fan" probe; then
    "$DEST/backend/ugreen-nas-ctl" start
else
    echo "WARNING: No supported fan controller found; fan control not started." >&2
fi

echo
"$DEST/backend/ugreen-nas-ctl" led-boot || true

echo "Installation complete."
