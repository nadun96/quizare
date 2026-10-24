#!/usr/bin/env bash
# First-time setup on an aaPanel (or any systemd) server, then the first
# release. Safe to run again: existing settings and the master key are kept.
#
#   1. a "quiz" system user and the folders under /www/quiz-app;
#   2. the master key (made once; back it up!);
#   3. the settings: your domain and aaPanel's PostgreSQL database;
#   4. a systemd service that starts the app at boot and restarts it if it stops;
#   5. the release, through update.sh.
#
# Usage (from the folder you unpacked install.sh into):
#   sudo bash install.sh quiz-<version>-linux-amd64.tar.gz
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=lib.sh
. "$here/lib.sh"
need_root "$@"
pkg=${1:-}
[ -f "$pkg" ] || die "usage: sudo bash install.sh quiz-<version>-linux-amd64.tar.gz"
command -v curl >/dev/null || die "curl is needed (apt install curl, or yum install curl)"

ask() { # ask VAR "Question" default
	local reply
	read -r -p "$2${3:+ [$3]}: " reply
	printf -v "$1" '%s' "${reply:-$3}"
}

# ---- 1. user and folders ----
if [ -z "${QP_TEST:-}" ] && ! id "$RUN_AS" >/dev/null 2>&1; then
	say "Creating the system user $RUN_AS"
	useradd --system --home-dir "$APP" --shell /usr/sbin/nologin "$RUN_AS"
fi
mkdir -p "$APP/releases" "$APP/config" "$APP/backups" "$APP/data/backups"
chmod 755 "$APP"
chmod 700 "$APP/config" "$APP/backups"
[ -n "${QP_TEST:-}" ] || chown -R "$RUN_AS:$RUN_AS" "$APP/data"

# ---- 2. master key ----
if [ ! -f "$APP/config/kek.key" ]; then
	say "Making the master key: $APP/config/kek.key"
	head -c 32 /dev/urandom >"$APP/config/kek.key"
	chmod 400 "$APP/config/kek.key"
	warn "Copy $APP/config/kek.key somewhere safe, off this server. Without it, stored API keys and secrets can't be read after a restore."
else
	say "Keeping the existing master key"
fi

# ---- 3. settings ----
if [ ! -f "$APP/config/quiz.env" ]; then
	echo
	echo "The website's address, as people will type it (the domain you add in aaPanel → Website):"
	ask domain "Domain, for example quiz.school.edu" "${QP_DOMAIN:-}"
	domain=${domain#https://}; domain=${domain#http://}; domain=${domain%%/*}
	[ -n "$domain" ] || die "a domain is needed"
	echo
	echo "The database you created in aaPanel → Databases → PgSQL:"
	ask dbname "Database name" "${QP_DB_NAME:-quiz}"
	ask dbuser "Database user" "${QP_DB_USER:-$dbname}"
	if [ -n "${QP_DB_PASSWORD:-}" ]; then dbpass=$QP_DB_PASSWORD; else read -r -s -p "Database password: " dbpass; echo; fi
	ask dbhost "Host" "${QP_DB_HOST:-127.0.0.1}"
	ask dbport "Port" "${QP_DB_PORT:-5432}"
	ask port "Port for the app (only Nginx reaches it)" "${QP_PORT:-8080}"
	url="postgres://$(urlencode "$dbuser"):$(urlencode "$dbpass")@$dbhost:$dbport/$(urlencode "$dbname")?sslmode=disable"
	psql=$(find_pg_tool psql)
	if [ -n "$psql" ]; then
		say "Checking the database"
		"$psql" "$url" -Atqc 'select version()' || die "can't sign in to the database with those details"
	fi
	umask 077
	cat >"$APP/config/quiz.env" <<EOF
# Settings for the quiz app. Read by systemd and by update.sh. Root only:
# it holds the database password. Restart after changes: systemctl restart $SERVICE
QP_DATABASE_URL="$url"
QP_BASE_URL="https://$domain"
QP_LISTEN="127.0.0.1:$port"
QP_STATIC_DIR="$APP/current/frontend"
QP_BACKUP_DIR="$APP/data/backups"
GOMEMLIMIT="700MiB"
# Optional: an SMTP relay for password-reset and verification emails.
#QP_SMTP_ADDR="smtp.example.edu:587"
#QP_SMTP_FROM="quiz@example.edu"
#QP_SMTP_USER="quiz"
#QP_SMTP_PASSWORD_FILE="$APP/config/smtp_password"
EOF
	umask 022
	chmod 600 "$APP/config/quiz.env"
	say "Settings saved in $APP/config/quiz.env"
else
	say "Keeping the existing settings in $APP/config/quiz.env"
fi

# ---- 4. the service ----
if [ -z "${QP_TEST:-}" ]; then
	say "Installing the $SERVICE service"
	cat >"/etc/systemd/system/$SERVICE.service" <<EOF
[Unit]
Description=Classroom Quiz
After=network-online.target postgresql.service pgsql.service
Wants=network-online.target

[Service]
Type=simple
User=$RUN_AS
Group=$RUN_AS
WorkingDirectory=$APP/data
EnvironmentFile=$APP/config/quiz.env
# The master key reaches the app as a credential, never an environment variable (ADR-09).
LoadCredential=kek:$APP/config/kek.key
ExecStart=$APP/current/server
Restart=always
RestartSec=2
TimeoutStopSec=20
MemoryMax=900M
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
PrivateDevices=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
RestrictSUIDSGID=true
LockPersonality=true
ReadWritePaths=$APP/data
CapabilityBoundingSet=
AmbientCapabilities=

[Install]
WantedBy=multi-user.target
EOF
	$SYSTEMCTL daemon-reload
	$SYSTEMCTL enable "$SERVICE" >/dev/null
fi

# ---- 5. the release ----
bash "$here/update.sh" "$pkg"

load_env
echo
say "Installed. Next steps (see the guide for details):"
echo "  1. aaPanel → Website → $( printf '%s' "${QP_BASE_URL#https://}" ): SSL (Let's Encrypt), then paste $APP/nginx.conf into its config."
echo "  2. Make the first admin:  sudo bash $APP/admin.sh you@school.edu \"Your Name\""
echo "  3. Back up $APP/config/kek.key, off this server."
