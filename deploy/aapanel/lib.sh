# Shared by install.sh, update.sh and admin.sh (sourced, not run).
#
# Where everything lives, outside aaPanel's web root so Nginx never serves
# it directly:
#   $APP/releases/<version>/   each release: server, frontend/, scripts
#   $APP/current               the running release (a symlink)
#   $APP/config/quiz.env       settings, with the database password (root only)
#   $APP/config/kek.key        the master key (root only; back it up!)
#   $APP/data/                 what the app writes (console backups)
#   $APP/backups/              database dumps taken before each update

APP=${QP_HOME:-/www/quiz-app}
SERVICE=${QP_SERVICE:-quiz}
SYSTEMCTL=${QP_SYSTEMCTL:-systemctl}   # tests replace it
RUN_AS=${QP_USER:-quiz}

say() { printf '\033[1m-> %s\033[0m\n' "$*"; }
warn() { printf '\033[33m!! %s\033[0m\n' "$*" >&2; }
die() { printf '\033[31mxx %s\033[0m\n' "$*" >&2; exit 1; }

need_root() {
	[ "$(id -u)" = 0 ] || [ -n "${QP_TEST:-}" ] || die "run this as root: sudo bash $0 $*"
}

# The settings, as the service sees them.
load_env() {
	[ -f "$APP/config/quiz.env" ] || die "no $APP/config/quiz.env: run install.sh first"
	set -a
	# shellcheck disable=SC1091
	. "$APP/config/quiz.env"
	set +a
}

# The port the app listens on, from QP_LISTEN (127.0.0.1:8080).
app_port() { printf '%s' "${QP_LISTEN##*:}"; }

# aaPanel puts PostgreSQL's tools under /www/server/pgsql/bin; its pg_dump
# matches its server, which a dump needs.
find_pg_tool() {
	local t=$1
	if [ -n "${QP_PG_BIN:-}" ] && [ -x "$QP_PG_BIN/$t" ]; then printf '%s' "$QP_PG_BIN/$t"; return; fi
	for d in /www/server/pgsql/bin /usr/lib/postgresql/*/bin /usr/pgsql-*/bin; do
		[ -x "$d/$t" ] && { printf '%s' "$d/$t"; return; }
	done
	command -v "$t" || true
}

# Waits for /healthz; true once the app answers.
healthy() {
	local port i
	port=$(app_port)
	for i in $(seq 1 "${1:-60}"); do
		if curl --max-time 3 -fsS "http://127.0.0.1:$port/healthz" >/dev/null 2>&1; then return 0; fi
		sleep 1
	done
	return 1
}

# Percent-encodes a value for a database URL (passwords with @, :, / …).
urlencode() {
	local s=$1 out='' c i
	for ((i = 0; i < ${#s}; i++)); do
		c=${s:i:1}
		case $c in
			[a-zA-Z0-9.~_-]) out+=$c ;;
			*) out+=$(printf '%%%02X' "'$c") ;;
		esac
	done
	printf '%s' "$out"
}
