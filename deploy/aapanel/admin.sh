#!/usr/bin/env bash
# Makes an admin account (the first one, or another). The password is
# asked for, never put on the command line.
#
# Usage: sudo bash /www/quiz-app/admin.sh you@school.edu "Your Name"
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=lib.sh
. "$here/lib.sh"
need_root "$@"
email=${1:-}
name=${2:-}
[ -n "$email" ] && [ -n "$name" ] || die "usage: sudo bash admin.sh you@school.edu \"Your Name\""
read -r -s -p "Password for $email (at least 8 characters): " pw
echo
[ ${#pw} -ge 8 ] || die "the password is too short"
# Run as the app's user, with the master key handed over as the service gets it.
printf '%s\n' "$pw" | systemd-run --pipe --wait --quiet \
	-p User="$RUN_AS" -p EnvironmentFile="$APP/config/quiz.env" -p LoadCredential=kek:"$APP/config/kek.key" \
	"$APP/current/server" create-admin "$email" "$name"
say "Admin $email can now sign in at $(. "$APP/config/quiz.env"; printf '%s' "$QP_BASE_URL")/login"
