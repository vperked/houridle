#!/usr/bin/env bash
# Installs houridle on a Linux server and starts it, in one go:
#
#   git clone https://github.com/vperked/houridle && cd houridle && ./install.sh
#
# It builds houridle from this folder if Go is installed, or else downloads
# the latest release. Then it installs it to ~/houridle with a service that
# starts it at boot, starts it, and prints the control panel's link: open it
# on your phone and sign in with the Steam app.
#
# Options:
#   --port N       serve the control panel on port N (default 8080)
#   --no-start     install, but don't start the service
#   --no-service   only install the program (no service; run it yourself)
#
# Run it as your normal user (it uses sudo only to keep your services
# running after you log out), or as root for system services.
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
install_dir="${HOURIDLE_DIR:-$HOME/houridle}"
repo="vperked/houridle"
port="" start=1 service=1

say() { printf '\n\033[1;36m◆\033[0m %s\n' "$*"; }
die() { printf '\n\033[1;31m✕\033[0m %s\n' "$*" >&2; exit 1; }

while [ $# -gt 0 ]; do
  case "$1" in
    --port) port="${2:?--port needs a number}"; shift 2 ;;
    --no-start) start=0; shift ;;
    --no-service) service=0 start=0; shift ;;
    -h | --help) sed -n '2,18p' "$0"; exit 0 ;;
    *) die "Unknown option $1 (see --help)" ;;
  esac
done
[ "$(uname -s)" = Linux ] || die "This installs houridle on Linux. On Windows, run build.bat (or download houridle.exe) and see the README."

# ---- 1. Get houridle ----

arch() {
  case "$(uname -m)" in
    x86_64 | amd64) echo amd64 ;;
    aarch64 | arm64) echo arm64 ;;
    *) die "No ready-made houridle for $(uname -m); install Go (https://go.dev/dl/) and Node.js (https://nodejs.org) and run this again to build it" ;;
  esac
}

# go_ok: Go is installed and new enough for go.mod.
go_ok() {
  command -v go >/dev/null || return 1
  local want have
  want="$(sed -n 's/^go \([0-9]*\.[0-9]*\).*/\1/p' "$here/go.mod")"
  have="$(go env GOVERSION | sed 's/^go\([0-9]*\.[0-9]*\).*/\1/')"
  [ "$(printf '%s\n%s\n' "$want" "$have" | sort -V | head -1)" = "$want" ]
}

build() {
  local version
  version="$(git -C "$here" describe --tags --always 2>/dev/null | sed 's/^v//' || true)"
  say "Building houridle ${version:+$version }with $(go env GOVERSION)"
  (cd "$here/frontend" && { [ -d node_modules ] || npm ci --no-audit --no-fund; } && npm run build)
  (cd "$here" && go build -trimpath -ldflags "-s -w -X main.version=${version:-dev}" -o "$1" ./backend/cmd/houridle)
}

# github_token: optional (the repository is public); it raises GitHub's
# limit on requests, and lets a private fork work.
github_token() {
  if [ -n "${GITHUB_TOKEN:-}" ]; then echo "$GITHUB_TOKEN"; return; fi
  if [ -n "${GH_TOKEN:-}" ]; then echo "$GH_TOKEN"; return; fi
  if command -v gh >/dev/null; then gh auth token 2>/dev/null && return; fi
  return 1
}

download() {
  local out="$1" name token api auth=() release id sums_id want got
  name="houridle-linux-$(arch)"
  command -v curl >/dev/null || die "Downloading needs curl (or install Go and Node.js to build houridle instead)"
  if token="$(github_token)"; then auth=(-H "Authorization: Bearer $token"); fi
  api="https://api.github.com/repos/$repo"
  say "Downloading the latest houridle release ($name)"
  release="$(curl -fsSL "${auth[@]}" -H "Accept: application/vnd.github+json" "$api/releases/latest")" ||
    die "Couldn't get the latest release from GitHub (there may not be one yet). Install Go (https://go.dev/dl/) and Node.js (https://nodejs.org) to build houridle here instead."
  asset_id() { printf '%s' "$release" | tr -d '\r\n' | tr ',' '\n' | grep -B3 "\"name\": *\"$1\"" | sed -n 's/.*"id": *\([0-9]*\).*/\1/p' | tail -1; }
  id="$(asset_id "$name")" sums_id="$(asset_id SHA256SUMS)"
  [ -n "$id" ] && [ -n "$sums_id" ] || die "The latest release has no $name or SHA256SUMS"
  curl -fsSL "${auth[@]}" -H "Accept: application/octet-stream" "$api/releases/assets/$id" -o "$out"
  want="$(curl -fsSL "${auth[@]}" -H "Accept: application/octet-stream" "$api/releases/assets/$sums_id" | awk -v n="$name" '$2 == n || $2 == "*"n {print $1}')"
  got="$(sha256sum "$out" | cut -d' ' -f1)"
  [ -n "$want" ] && [ "$want" = "$got" ] || { rm -f "$out"; die "The download doesn't match SHA256SUMS; not installing it"; }
  chmod 755 "$out"
}

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
binary="$tmp/houridle"
if [ -f "$here/go.mod" ] && go_ok && command -v npm >/dev/null; then
  build "$binary"
else
  download "$binary"
fi

# ---- 2. Install it, with its service ----

if [ "$service" = 0 ]; then
  say "Installing houridle to $install_dir"
  mkdir -p "$install_dir"
  install -m 755 "$binary" "$install_dir/houridle"
else
  HOURIDLE_DIR="$install_dir" bash "$here/deploy/linux/setup-signin.sh" "$binary" >/dev/null
  say "Installed houridle to $install_dir, with a service that starts it at boot"
fi
if [ -n "$port" ]; then
  "$install_dir/houridle" web ":$port" >/dev/null
fi

# ---- 3. Start it and show the link ----

if [ "$(id -u)" -eq 0 ]; then ctl="systemctl"; else ctl="systemctl --user"; fi
if [ "$start" = 1 ]; then
  say "Starting houridle"
  $ctl enable houridle-signin >/dev/null 2>&1
  $ctl restart houridle-signin
  for _ in $(seq 20); do
    grep -q "control panel on" "$install_dir/houridle.log" 2>/dev/null && break
    sleep 0.5
  done
  $ctl is-active --quiet houridle-signin || die "The service didn't start. See: $ctl status houridle-signin"
fi

say "Done."
"$install_dir/houridle" web
cat <<NEXT

  Sign in with the Steam app (More → Accounts), then pick what to idle
  (Idle). houridle carries on after a reboot, and keeps itself up to date.

  Check on it:  tail -f $install_dir/houridle.log
NEXT
if [ "$start" = 0 ] && [ "$service" = 1 ]; then echo "  Start it:     $ctl enable --now houridle-signin"; fi
if [ "$service" = 0 ]; then echo "  Start it:     $install_dir/houridle serve"; fi
