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

# ---- tutoring (tutoring.sh, D-91) ----

# Whether nothing listens on a port: port_free 8090 tcp (or udp).
port_free() {
	command -v ss >/dev/null || return 0
	[ -z "$(ss -Hln --"$2" "sport = :$1" 2>/dev/null)" ]
}

# Which program listens on a port, for the message.
port_user() {
	ss -Hlnp --"$2" "sport = :$1" 2>/dev/null | sed -n 's/.*users:((\("[^"]*"\).*/\1/p' | head -1
}

tutoring_on() { [ -f "$APP/config/tutor.env" ]; }

# Tutoring's settings, as its service sees them.
tutor_env() {
	set -a
	# shellcheck disable=SC1091
	. "$APP/config/tutor.env"
	set +a
}

# Sets KEY="value" in quiz.env, replacing the line if there is one.
set_env() {
	local f="$APP/config/quiz.env" tmp
	tmp=$(mktemp "$APP/config/.quiz.env.XXXXXX")
	grep -v "^$1=" "$f" >"$tmp" || true
	printf '%s="%s"\n' "$1" "$2" >>"$tmp"
	chmod 600 "$tmp"
	mv -f "$tmp" "$f"
	printf -v "$1" '%s' "$2"
	export "${1?}"
}

# Removes KEY from quiz.env.
unset_env() {
	local f="$APP/config/quiz.env" tmp
	tmp=$(mktemp "$APP/config/.quiz.env.XXXXXX")
	grep -v "^#\?$1=" "$f" >"$tmp" || true
	chmod 600 "$tmp"
	mv -f "$tmp" "$f"
	unset "$1"
}

# LiveKit's media ports, "TCP UDP", from its settings.
livekit_ports() {
	local tcp udp
	tcp=$(sed -n 's/^ *tcp_port: *\([0-9]*\).*/\1/p' "$APP/config/livekit.yaml" | head -1)
	udp=$(sed -n 's/^ *udp_port: *\([0-9]*\).*/\1/p' "$APP/config/livekit.yaml" | head -1)
	printf '%s %s' "$tcp" "$udp"
}

# Waits for the tutoring service's /healthz.
tutor_healthy() {
	local port i
	tutor_env
	port=${TUTOR_LISTEN##*:}
	for i in $(seq 1 "${1:-60}"); do
		if curl --max-time 3 -fsS "http://127.0.0.1:$port/healthz" >/dev/null 2>&1; then return 0; fi
		sleep 1
	done
	return 1
}

# Writes the Nginx configs to paste into aaPanel from the running release's
# samples: the app's with its port and, with tutoring on, tutoring's address
# allowed (the pages call it and play recordings from it), and the tutoring
# subdomain's.
render_nginx() {
	local tmp lk t
	[ -f "$APP/current/nginx.conf" ] || return 0
	tmp=$(mktemp "$APP/.nginx.conf.XXXXXX")
	sed "s#127\.0\.0\.1:8080#127.0.0.1:$(app_port)#; s#Change 8080 if you chose another port in install.sh.#The app's port, from QP_LISTEN, is filled in by update.sh.#" "$APP/current/nginx.conf" >"$tmp"
	if tutoring_on && [ -f "$APP/current/nginx-tutor.conf" ]; then
		tutor_env
		t="https://$TUTOR_DOMAIN"
		sed -i "s#connect-src 'self' wss:#connect-src 'self' wss: $t#; s#object-src 'none'#media-src 'self' $t; object-src 'none'#" "$tmp"
		lk=$(livekit_ports)
		sed "s#__TUTOR_PORT__#${TUTOR_LISTEN##*:}#g; s#__LIVEKIT_PORT__#${LIVEKIT_URL##*:}#g; s#__LIVEKIT_TCP__#${lk% *}#g; s#__LIVEKIT_UDP__#${lk#* }#g" \
			"$APP/current/nginx-tutor.conf" >"$APP/.nginx-tutor.conf.new"
		chmod 644 "$APP/.nginx-tutor.conf.new"
		mv -f "$APP/.nginx-tutor.conf.new" "$APP/nginx-tutor.conf"
	fi
	chmod 644 "$tmp"
	mv -f "$tmp" "$APP/nginx.conf"
}
