#!/usr/bin/env bash
# Installs a new release built with scripts/package.ps1, keeping all data:
#
#   1. dumps the database (pg_dump), so an update can always be undone;
#   2. unpacks the release beside the running one;
#   3. points "current" at it and restarts the app, which applies any
#      database changes (migrations) itself as it starts;
#   4. waits until it answers; if it doesn't, switches back to the previous
#      release and restarts that.
#
# Sessions are kept in the database, so nobody is signed out. Live quizzes
# and meetings reconnect by themselves after the few seconds of restart.
# With tutoring on (tutoring.sh), its service restarts too, and LiveKit when
# the release brings a new one: live tutoring sessions then drop, so update
# between lessons.
#
# Usage: sudo bash update.sh quiz-<version>-linux-amd64.tar.gz
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=lib.sh
. "$here/lib.sh"
need_root "$@"
pkg=${1:-}
[ -f "$pkg" ] || die "usage: sudo bash update.sh quiz-<version>-linux-amd64.tar.gz"
pkg=$(cd "$(dirname "$pkg")" && pwd)/$(basename "$pkg")
if [ -f "$pkg.sha256" ]; then
	(cd "$(dirname "$pkg")" && sha256sum -c --quiet "$(basename "$pkg").sha256") || die "the package doesn't match its .sha256: upload it again"
fi
load_env

# ---- 1. unpack and check ----
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
tar -xzf "$pkg" -C "$work"
[ -f "$work/server" ] && [ -f "$work/frontend/index.html" ] && [ -f "$work/VERSION" ] || die "$pkg isn't a release package"
# Packages built on Windows don't carry Unix permissions.
chmod 755 "$work/server"
for f in tutor livekit-server; do [ ! -f "$work/$f" ] || chmod 755 "$work/$f"; done
find "$work" -name '*.sh' -exec chmod 755 {} +
version=$(tr -dc 'A-Za-z0-9._-' <"$work/VERSION")
dest="$APP/releases/$version"
[ -e "$dest" ] && dest="$dest-$(date +%Y%m%d%H%M%S)"
previous=$(readlink "$APP/current" 2>/dev/null || true)
say "Updating to $version${previous:+ (from $(basename "$previous"))}"

# ---- 2. back up the database ----
mkdir -p "$APP/backups"
chmod 700 "$APP/backups"
if [ -n "${QP_SKIP_BACKUP:-}" ]; then
	warn "QP_SKIP_BACKUP is set: no database backup this time"
else
	pg_dump=$(find_pg_tool pg_dump)
	[ -n "$pg_dump" ] || die "pg_dump not found. Install PostgreSQL's client tools, set QP_PG_BIN to their folder, or (not recommended) set QP_SKIP_BACKUP=1"
	dump="$APP/backups/before-$version-$(date +%Y%m%d-%H%M%S).dump"
	say "Backing up the database to $dump"
	"$pg_dump" --format=custom --no-owner --dbname="$QP_DATABASE_URL" --file="$dump" || die "the backup failed, so nothing was changed"
	chmod 600 "$dump"
	# Keep the last 10 backups.
	ls -1t "$APP"/backups/before-*.dump 2>/dev/null | tail -n +11 | xargs -r rm -f
fi

# ---- 3. switch and restart ----
mkdir -p "$APP/releases"
mv "$work" "$dest"
trap - EXIT
[ -n "${QP_TEST:-}" ] || chown -R root:root "$dest"
chmod 755 "$dest" "$dest/server"
# The scripts beside the app are always the newest ones. Renamed into place,
# never copied over: this very script may be one of them, still being read.
for f in update.sh install.sh admin.sh lib.sh tutoring.sh; do
	if [ -f "$dest/$f" ]; then
		cp "$dest/$f" "$APP/.$f.new"
		mv -f "$APP/.$f.new" "$APP/$f"
	fi
done
ln -sfn "$dest" "$APP/current.new"
mv -Tf "$APP/current.new" "$APP/current"
# The Nginx configs to paste, from this release's samples.
render_nginx
say "Restarting the app (database changes are applied as it starts)"
$SYSTEMCTL restart "$SERVICE"

# Tutoring follows the release too. LiveKit restarts only when it changed,
# since that drops the video of live sessions.
restart_tutoring() {
	tutoring_on || return 0
	if [ -z "$previous" ] || ! cmp -s "$previous/livekit-server" "$APP/current/livekit-server"; then
		say "Restarting LiveKit (a new version)"
		$SYSTEMCTL restart quiz-livekit
	fi
	say "Restarting the tutoring service"
	$SYSTEMCTL restart quiz-tutoring
	tutor_healthy 60 || { warn "The tutoring service didn't start. Its log: journalctl -u quiz-tutoring -n 80"; return 1; }
}

# ---- 4. check, or go back ----
if healthy 90 && restart_tutoring; then
	say "Running $version"
	# Keep the last 5 releases.
	ls -1dt "$APP"/releases/*/ 2>/dev/null | tail -n +6 | while read -r old; do
		[ "$(readlink -f "$old")" = "$(readlink -f "$APP/current")" ] || rm -rf "$old"
	done
	exit 0
fi
warn "$version didn't start. The log: journalctl -u $SERVICE -n 80"
if [ -n "$previous" ] && [ -d "$previous" ]; then
	ln -sfn "$previous" "$APP/current.new"
	mv -Tf "$APP/current.new" "$APP/current"
	render_nginx
	$SYSTEMCTL restart "$SERVICE"
	if tutoring_on; then
		$SYSTEMCTL restart quiz-livekit
		$SYSTEMCTL restart quiz-tutoring
	fi
	if healthy 60; then
		warn "Went back to $(basename "$previous"); it's running."
		warn "If the new version changed the database before failing and the old one misbehaves, restore the backup: see the guide."
	else
		warn "The previous release doesn't start either. Restore the backup: see the guide."
	fi
fi
exit 1
