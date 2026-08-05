#!/usr/bin/env bash
set -euo pipefail

# Builds stcompare and stbench and installs them onto $PATH so they can be
# run as `stcompare <command>` / `stbench <command>` from any repository.
#
# Install location precedence: --dir/-d flag, $INSTALL_DIR, $GOBIN,
# $(go env GOPATH)/bin.

usage() {
  echo "Usage: $0 [-d|--dir INSTALL_DIR]" >&2
}

INSTALL_DIR="${INSTALL_DIR:-}"

while [ $# -gt 0 ]; do
  case "$1" in
    -d|--dir)
      INSTALL_DIR="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      usage
      exit 1
      ;;
  esac
done

if [ -z "$INSTALL_DIR" ]; then
  INSTALL_DIR="${GOBIN:-}"
fi
if [ -z "$INSTALL_DIR" ]; then
  INSTALL_DIR="$(go env GOPATH)/bin"
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

mkdir -p "$INSTALL_DIR"

echo "Building stcompare and stbench into $INSTALL_DIR ..."
(cd "$REPO_ROOT" && go build -o "$INSTALL_DIR/stcompare" ./cmd/stcompare)
(cd "$REPO_ROOT" && go build -o "$INSTALL_DIR/stbench" ./cmd/stbench)

echo "Installed:"
echo "  $INSTALL_DIR/stcompare"
echo "  $INSTALL_DIR/stbench"

case ":$PATH:" in
  *":$INSTALL_DIR:"*)
    echo "$INSTALL_DIR is already on your PATH."
    ;;
  *)
    echo
    echo "$INSTALL_DIR is not on your PATH. Add it, e.g.:"
    echo "  export PATH=\"$INSTALL_DIR:\$PATH\""
    ;;
esac
