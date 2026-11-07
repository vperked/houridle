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
# Run it again any time to update. It finds the houridle already installed
# (wherever it is, and whichever service runs it), changes only what
# differs, and restarts houridle only if something did. Settings, sign-ins
# and history stay as they are. If the installed houridle has updated itself
# past this one, that one is kept.
#
# Options:
#   --port N       serve the control panel on port N (default 8080)
#   --no-start     install, but don't start the service
#   --no-service   only install the program (no service; run it yourself)
#   --force        install this houridle even if the installed one is newer
#
# Run it as your normal user (it uses sudo only to keep your services
# running after you log out), or as root for system services.
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
install_dir="${HOURIDLE_DIR:-$HOME/houridle}"
repo="vperked/houridle"
port="" start=1 service=1 force=0
given="$*"

say() { printf '\n\033[1;36m◆\033[0m %s\n' "$*"; }
die() { printf '\n\033[1;31m✕\033[0m %s\n' "$*" >&2; exit 1; }

while [ $# -gt 0 ]; do
  case "$1" in
    --port) port="${2:?--port needs a number}"; shift 2 ;;
    --no-start) start=0; shift ;;
    --no-service) service=0 start=0; shift ;;
    --force) force=1; shift ;;
    -h | --help) awk 'NR > 1 && /^set -euo/ { exit } NR > 1' "$0"; exit 0 ;;
    *) die "Unknown option $1 (see --help)" ;;
  esac
done
[ "$(uname -s)" = Linux ] || die "This installs houridle on Linux. On Windows, run build.bat (or download houridle.exe) and see the README."

# ---- 1. What's installed already ----

if [ "$(id -u)" -eq 0 ]; then
  ctl="systemctl" unit_dir=/etc/systemd/system
else
  ctl="systemctl --user" unit_dir="$HOME/.config/systemd/user"
fi
unit="$unit_dir/houridle-signin.service"

# An install made as root runs as a system service. Installing as a normal
# user next to it would start a second houridle, so update that one instead.
if [ "$(id -u)" -ne 0 ] && [ -f /etc/systemd/system/houridle-signin.service ] && [ ! -f "$unit" ]; then
  die "houridle is installed here as a system service (by root). To update it, run: sudo $0 $given"
fi

# The service knows where an earlier install went (HOURIDLE_DIR may have
# moved it from ~/houridle).
if [ -z "${HOURIDLE_DIR:-}" ] && [ -f "$unit" ]; then
  dir="$(sed -n 's/^WorkingDirectory=//p' "$unit" | head -n1)"
  if [ -n "$dir" ] && [ -x "$dir/houridle" ]; then install_dir="$dir"; fi
fi

# version_of prints a houridle program's version, or nothing if it can't
# tell. Every version's help starts with a banner that has it (-h only
# prints; it never starts idling).
version_of() {
  { timeout 10 "$1" -h 2>&1 || true; } | sed -n 's/.*idler · v\([0-9][^ ]*\).*/\1/p' | head -n1
}

# newer: is version $1 later than $2? Only real versions compare (1.6.2,
# 1.7.0-3-gabc1234, as releases and git builds have).
newer() {
  [[ "$1" =~ ^[0-9]+\.[0-9]+ ]] && [[ "$2" =~ ^[0-9]+\.[0-9]+ ]] || return 1
  [ "$1" != "$2" ] && [ "$(printf '%s\n%s\n' "$1" "$2" | sort -V | tail -n1)" = "$1" ]
}

installed=0 running=0 client_mode=0 old_version=""
if [ -x "$install_dir/houridle" ]; then
  installed=1
  old_version="$(version_of "$install_dir/houridle")"
  if $ctl is-active --quiet houridle-signin 2>/dev/null; then running=1; fi
  # deploy/linux/setup.sh's way: idling through a Steam client on a virtual
  # display (once its services are disabled, this sets up the control panel).
  if [ ! -f "$unit" ] && [ -f "$unit_dir/houridle.service" ] && $ctl is-enabled --quiet houridle.service 2>/dev/null; then client_mode=1; fi
  state="no service"
  if [ "$client_mode" = 1 ]; then
    state="idling through the Steam client"
  elif [ -f "$unit" ]; then
    state="$([ "$running" = 1 ] && echo running || echo "service not running")"
  fi
  say "Found houridle ${old_version:-(unknown version)} in $install_dir ($state)"
fi

# ---- 2. Get houridle ----

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

# build <version> <out>. The page's packages are installed again only when
# package-lock.json changed since the last time (a pull that updates them),
# not on every build; a copy of the lockfile in node_modules marks which.
build() {
  say "Building houridle ${1:+$1 }with $(go env GOVERSION)"
  (
    cd "$here/frontend"
    if ! cmp -s package-lock.json node_modules/.houridle-lock 2>/dev/null; then
      npm ci --prefer-offline --no-audit --no-fund
      cp package-lock.json node_modules/.houridle-lock
    fi
    npm run build
  )
  (cd "$here" && go build -trimpath -ldflags "-s -w -X main.version=${1:-dev}" -o "$2" ./backend/cmd/houridle)
}

# github_token: optional (the repository is public); it raises GitHub's
# limit on requests, and lets a private fork work.
github_token() {
  if [ -n "${GITHUB_TOKEN:-}" ]; then echo "$GITHUB_TOKEN"; return; fi
  if [ -n "${GH_TOKEN:-}" ]; then echo "$GH_TOKEN"; return; fi
  if command -v gh >/dev/null; then gh auth token 2>/dev/null && return; fi
  return 1
}

# download <out> gets the latest release, checked against its SHA256SUMS.
# If that release (or a newer houridle) is installed already, it downloads
# nothing and points binary at the installed one.
download() {
  local out="$1" name token api auth=() release tag id sums_id want got
  name="houridle-linux-$(arch)"
  command -v curl >/dev/null || die "Downloading needs curl (or install Go and Node.js to build houridle instead)"
  if token="$(github_token)"; then auth=(-H "Authorization: Bearer $token"); fi
  api="https://api.github.com/repos/$repo"
  release="$(curl -fsSL "${auth[@]}" -H "Accept: application/vnd.github+json" "$api/releases/latest")" ||
    die "Couldn't get the latest release from GitHub (there may not be one yet). Install Go (https://go.dev/dl/) and Node.js (https://nodejs.org) to build houridle here instead."
  tag="$(printf '%s' "$release" | tr -d '\r\n' | tr ',' '\n' | sed -n 's/.*"tag_name": *"v\{0,1\}\([^"]*\)".*/\1/p' | head -n1)"
  if [ "$installed" = 1 ] && [ "$force" = 0 ] && [ -n "$tag" ] && { [ "$tag" = "$old_version" ] || newer "$old_version" "$tag"; }; then
    say "houridle $old_version is installed and the latest release is $tag; nothing to download"
    binary="$install_dir/houridle"
    return
  fi
  say "Downloading houridle ${tag:-(latest release)} ($name)"
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
  # The version is the commit (or tag) it's built from; --dirty marks
  # uncommitted changes, so such a build never passes for its commit.
  src_version="$(git -C "$here" describe --tags --always --dirty 2>/dev/null | sed 's/^v//' || true)"
  if [ "$installed" = 1 ] && [ "$force" = 0 ] && [ -n "$src_version" ] && [ "$src_version" = "$old_version" ] && [ "${src_version%-dirty}" = "$src_version" ]; then
    say "houridle $old_version is installed, built from this same code; nothing to build"
    binary="$install_dir/houridle"
  else
    build "$src_version" "$binary"
  fi
else
  download "$binary"
fi

# houridle keeps itself up to date, so the installed one can be the newer.
new_version="$(version_of "$binary")"
if [ "$installed" = 1 ] && [ "$force" = 0 ] && newer "$old_version" "$new_version"; then
  say "Keeping houridle $old_version: it has updated itself past this one ($new_version). --force installs this one anyway."
  binary="$install_dir/houridle"
fi

# ---- 3. Install it, with its service ----

# fingerprint stands for the program and its services, to tell whether this
# run changed anything.
fingerprint() {
  { cat "$install_dir/houridle" "$unit" "$unit_dir/houridle-signin@.service" 2>/dev/null || true; } | sha256sum | cut -d' ' -f1
}
before="$(fingerprint)"

if [ "$client_mode" = 1 ]; then
  # Only the program changes; its services (display, Steam, houridle) stay.
  if cmp -s "$binary" "$install_dir/houridle"; then
    say "Done. houridle ${old_version:-} is up to date; nothing to change."
  else
    install -m 755 "$binary" "$install_dir/houridle"
    systemctl --user try-restart houridle.service
    say "Done. Updated houridle ${old_version:-} → $(version_of "$install_dir/houridle"), and restarted it if it was running."
  fi
  cat <<NOTE

  This server idles through the Steam client (deploy/linux/setup.sh), so
  install.sh only updated the program. To switch to the control panel and a
  saved sign-in instead, stop that first (systemctl --user disable --now
  houridle houridle-steam houridle-display), then run this again.
NOTE
  exit 0
fi

if [ "$service" = 0 ]; then
  say "Installing houridle to $install_dir"
  mkdir -p "$install_dir"
  if ! cmp -s "$binary" "$install_dir/houridle"; then install -m 755 "$binary" "$install_dir/houridle"; fi
else
  HOURIDLE_DIR="$install_dir" bash "$here/deploy/linux/setup-signin.sh" "$binary" >/dev/null
  if [ "$installed" = 0 ]; then say "Installed houridle to $install_dir, with a service that starts it at boot"; fi
fi
changed=0
if [ "$(fingerprint)" != "$before" ]; then changed=1; fi
if [ -n "$port" ]; then
  "$install_dir/houridle" web ":$port" >/dev/null
  changed=1 # the running houridle reads the port when it starts
fi

# ---- 4. Start it (or restart it, if it changed) and show the link ----

log="$install_dir/houridle.log"
if [ "$start" = 1 ]; then
  $ctl enable houridle-signin >/dev/null 2>&1
  if [ "$running" = 1 ] && [ "$changed" = 0 ]; then
    say "houridle is already running this version; left it alone"
  else
    if [ "$running" = 1 ]; then say "Restarting houridle (it carries on where it left off)"; else say "Starting houridle"; fi
    seen=0 # log lines from before, so an old "control panel on" doesn't count
    if [ -f "$log" ]; then seen="$(wc -l <"$log")"; fi
    $ctl restart houridle-signin
    for _ in $(seq 20); do
      tail -n +"$((seen + 1))" "$log" 2>/dev/null | grep -q "control panel on" && break
      sleep 0.5
    done
    $ctl is-active --quiet houridle-signin || die "The service didn't start. See: $ctl status houridle-signin"
  fi
elif [ "$running" = 1 ] && [ "$changed" = 1 ]; then
  say "houridle is still running the old version. To switch: $ctl restart houridle-signin"
fi
# Accounts idling on their own (houridle-signin@<name>) move to the new
# program too. try-restart leaves stopped ones stopped.
if [ "$changed" = 1 ] && [ "$service" = 1 ]; then
  $ctl try-restart 'houridle-signin@*.service' >/dev/null 2>&1 || true
fi

if [ "$installed" = 0 ]; then
  say "Done."
elif [ "$changed" = 0 ]; then
  say "Done. houridle ${old_version:-} is up to date; nothing to change."
else
  now="$(version_of "$install_dir/houridle")"
  if newer "$old_version" "$now"; then
    say "Done. Went back from houridle $old_version to $now (--force)."
  elif [ -n "$now" ] && [ "$now" != "$old_version" ]; then
    say "Done. Updated houridle ${old_version:-} → $now."
  else
    say "Done. houridle ${now:-} is up to date; its setup was adjusted."
  fi
fi
"$install_dir/houridle" web
if [ "$installed" = 0 ]; then
  cat <<NEXT

  Sign in with the Steam app (More → Accounts), then pick what to idle
  (Idle). houridle carries on after a reboot, and keeps itself up to date.
NEXT
fi
cat <<NEXT

  Check on it:  tail -f $log
NEXT
if [ "$start" = 0 ] && [ "$service" = 1 ] && [ "$running" = 0 ]; then echo "  Start it:     $ctl enable --now houridle-signin"; fi
if [ "$service" = 0 ] && [ ! -f "$unit" ]; then echo "  Start it:     $install_dir/houridle serve"; fi
