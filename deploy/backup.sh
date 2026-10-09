#!/usr/bin/env bash
# Nightly encrypted off-site backup (architecture §5 step 4, R11).
# Keeps 14 daily and 8 weekly copies. Test a restore every month:
#   age -d -i /etc/quiz/backup.key quiz-YYYYmmdd.dump.age | pg_restore -d quiz_restore_test
# Schedule with deploy/quiz-backup.timer. Requires: pg_dump, age, rclone (configured remote "offsite").
#
# The admin console shows these backups and the last run (PL-FR-08, D-57):
# set QP_NIGHTLY_BACKUP_DIR=/var/backups/quiz for the server. Files and the
# status folder are readable by the "quiz" group so the server can list and
# offer them for download; they stay encrypted with the offline age key.
set -euo pipefail

DB=${DB:-quiz}
DIR=${DIR:-/var/backups/quiz}
RECIPIENT_FILE=${RECIPIENT_FILE:-/etc/quiz/backup.pub}   # age public key; the private key is kept offline
REMOTE=${REMOTE:-offsite:quiz-backups}

APP_GROUP=${APP_GROUP:-quiz}
umask 027
mkdir -p "$DIR/daily" "$DIR/weekly" "$DIR/status"
status() { echo "$1 $(date -u +%Y-%m-%dT%H:%M:%SZ)" > "$DIR/status/last-run"; }
trap 'status failed' ERR
stamp=$(date +%Y%m%d)
out="$DIR/daily/quiz-$stamp.dump.age"

pg_dump -Fc "$DB" | age -R "$RECIPIENT_FILE" -o "$out"
if [ "$(date +%u)" = 7 ]; then cp "$out" "$DIR/weekly/"; fi

ls -1t "$DIR/daily"/*.age | tail -n +15 | xargs -r rm --
ls -1t "$DIR/weekly"/*.age | tail -n +9 | xargs -r rm --

rclone sync "$DIR" "$REMOTE"
status ok
touch "$DIR/status/last-success"
getent group "$APP_GROUP" >/dev/null && chgrp -R "$APP_GROUP" "$DIR" || true
echo "backup ok: $out"
