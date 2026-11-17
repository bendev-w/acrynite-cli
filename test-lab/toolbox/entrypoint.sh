#!/usr/bin/env bash
# Toolbox entrypoint: install the acrynite binary into the shared folder and set the
# permissions that let PostgreSQL (uid/gid 999) run it and read the storage area.
set -euo pipefail

: "${LAB_WORK:?LAB_WORK must be set}"

# umask 0002 + setgid + group 999 keep every file Acrynite creates under work/ readable
# (and the binary executable) by the postgres OS user, which runs archive_command.
umask 0002
mkdir -p "$LAB_WORK"/bin "$LAB_WORK"/storage "$LAB_WORK"/tmp "$LAB_WORK"/home
chgrp -R 999 "$LAB_WORK" 2>/dev/null || true
chmod -R g+rwX "$LAB_WORK" 2>/dev/null || true
find "$LAB_WORK" -type d -exec chmod g+s {} + 2>/dev/null || true

install_from_release() {
  echo "Installing acrynite (${ACRYNITE_VERSION:-latest}) into $LAB_WORK/bin ..."
  curl -fsSL https://raw.githubusercontent.com/bendev-w/acrynite-cli/main/install.sh \
    | ACRYNITE_INSTALL_DIR="$LAB_WORK/bin" ACRYNITE_VERSION="${ACRYNITE_VERSION:-latest}" sh
}

TARGET="$LAB_WORK/bin/acrynite"

if [ -n "${ACRYNITE_BINARY:-}" ] && [ -f "${ACRYNITE_BINARY}" ]; then
  # Use a locally built binary — but only one that lives under LAB_WORK, so the same
  # absolute path exists inside the postgres container too.
  case "$ACRYNITE_BINARY" in
    "$LAB_WORK"/*) : ;;
    *) echo "ACRYNITE_BINARY must be a path under $LAB_WORK" >&2; exit 1 ;;
  esac
  if [ "$(readlink -f "$ACRYNITE_BINARY")" != "$(readlink -f "$TARGET" 2>/dev/null || echo "")" ]; then
    install -m 0775 "$ACRYNITE_BINARY" "$TARGET"
  fi
  echo "Using provided binary: $ACRYNITE_BINARY"
elif [ ! -x "$TARGET" ]; then
  install_from_release
fi

chmod 0775 "$TARGET" 2>/dev/null || true

# Put acrynite on PATH for every `docker compose exec` shell. os.Executable() follows the
# symlink to the real file at $TARGET, so archive_command still names the shared-folder path.
ln -sf "$TARGET" /usr/local/bin/acrynite

"$TARGET" --version 2>/dev/null || echo "warning: acrynite --version did not run; check the install step above"

exec "$@"
