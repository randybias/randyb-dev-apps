#!/usr/bin/env bash
set -euo pipefail

REPO_SLUG="randybias/randyb-dev-apps"
REPO_SSH="git@github.com:${REPO_SLUG}.git"
INSTALL_DIR="${PORTWARDEN_SRC_DIR:-${HOME}/.local/share/randyb-dev-apps}"
BIN_DIR="${PORTWARDEN_BIN_DIR:-${HOME}/.local/bin}"
APP="portwarden"

log() { printf '[install] %s\n' "$*"; }
die() { printf '[install] error: %s\n' "$*" >&2; exit 1; }

require() {
  command -v "$1" >/dev/null 2>&1 || die "missing required tool: $1"
}

clone_or_update() {
  if [ -d "${INSTALL_DIR}/.git" ]; then
    log "updating ${INSTALL_DIR}"
    git -C "${INSTALL_DIR}" pull --ff-only
  else
    log "cloning ${REPO_SLUG} into ${INSTALL_DIR}"
    mkdir -p "$(dirname "${INSTALL_DIR}")"
    if command -v gh >/dev/null 2>&1; then
      gh repo clone "${REPO_SLUG}" "${INSTALL_DIR}"
    else
      git clone "${REPO_SSH}" "${INSTALL_DIR}"
    fi
  fi
}

build_app() {
  log "building ${APP}"
  ( cd "${INSTALL_DIR}" && make build APP="${APP}" )
}

install_bin() {
  mkdir -p "${BIN_DIR}"
  install -m 0755 "${INSTALL_DIR}/bin/${APP}" "${BIN_DIR}/${APP}"
  log "installed ${BIN_DIR}/${APP}"
}

register_mcp() {
  if command -v claude >/dev/null 2>&1; then
    log "registering ${APP} MCP server (user scope)"
    claude mcp add "${APP}" --scope user -- "${BIN_DIR}/${APP}" 2>/dev/null \
      || log "claude mcp add skipped (already registered?)"
  else
    log "claude CLI not found; register manually: claude mcp add ${APP} -- ${BIN_DIR}/${APP}"
  fi
}

main() {
  require git
  require go
  if [ -f "./${APP}/main.go" ] && [ -f "./go.mod" ]; then
    INSTALL_DIR="$(pwd)"
    log "using current checkout at ${INSTALL_DIR}"
  else
    clone_or_update
  fi
  build_app
  install_bin
  register_mcp
  log "done. ensure ${BIN_DIR} is on your PATH."
}

main "$@"
