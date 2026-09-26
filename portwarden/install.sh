#!/usr/bin/env bash
set -euo pipefail

REPO_SLUG="randybias/randyb-dev-apps"
REPO_URL="https://github.com/${REPO_SLUG}.git"
SRC_DIR_OVERRIDE="${PORTWARDEN_SRC_DIR:-}"
INSTALL_DIR="${SRC_DIR_OVERRIDE:-${HOME}/.local/share/randyb-dev-apps}"
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
    if command -v gh >/dev/null 2>&1 && gh repo clone "${REPO_SLUG}" "${INSTALL_DIR}"; then
      return
    fi
    [ -d "${INSTALL_DIR}" ] && [ -n "$(ls -A "${INSTALL_DIR}")" ] \
      && die "${INSTALL_DIR} exists and is not empty; remove it or set PORTWARDEN_SRC_DIR"
    log "cloning ${REPO_URL}"
    git clone "${REPO_URL}" "${INSTALL_DIR}"
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
    if claude mcp get "${APP}" >/dev/null 2>&1; then
      log "${APP} MCP server already registered"
      return
    fi
    local out
    if ! out="$(claude mcp add "${APP}" --scope user -- "${BIN_DIR}/${APP}" 2>&1)"; then
      log "warning: claude mcp add failed: ${out}"
      log "register manually: claude mcp add ${APP} --scope user -- ${BIN_DIR}/${APP}"
    fi
  else
    log "claude CLI not found; register manually: claude mcp add ${APP} --scope user -- ${BIN_DIR}/${APP}"
  fi
}

# local_checkout prints the repo root when this script is run from a file inside
# a checkout (any cwd); prints nothing when piped to bash.
local_checkout() {
  local src="${BASH_SOURCE[0]:-}"
  [ -n "${src}" ] && [ -f "${src}" ] || return 0
  local root
  root="$(cd "$(dirname "${src}")/.." && pwd)"
  if [ -f "${root}/${APP}/main.go" ] && [ -f "${root}/go.mod" ]; then
    printf '%s\n' "${root}"
  fi
}

main() {
  require git
  require go
  local checkout
  checkout="$(local_checkout)"
  if [ -z "${SRC_DIR_OVERRIDE}" ] && [ -n "${checkout}" ]; then
    INSTALL_DIR="${checkout}"
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
