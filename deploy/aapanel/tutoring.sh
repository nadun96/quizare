#!/usr/bin/env bash
# Switches tutoring on (D-59, D-91): the tutoring service and LiveKit, its
# media server, beside the app installed by install.sh, from the same
# release. Safe to run again: existing secrets and settings are kept.
#
#   1. the secret shared by the app and the tutoring service, and LiveKit's key;
#   2. the settings: the tutoring subdomain and ports;
#   3. two systemd services, quiz-livekit and quiz-tutoring, and the app's
#      share of the secret;
#   4. restarts the app with tutoring on, and starts both;
#   5. the Nginx config for the subdomain, and the app's, with tutoring's
#      address allowed.
#
# The tutoring service keeps its tables in the app's database, in their own
# "tutoring" schema, so it needs no database of its own.
#
# Usage: sudo bash /www/quiz-app/tutoring.sh
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=lib.sh
. "$here/lib.sh"
need_root "$@"
load_env
cur="$APP/current"
[ -x "$cur/tutor" ] && [ -x "$cur/livekit-server" ] || die "this release has no tutoring service: build a newer package (scripts/package.ps1) and run update.sh with it first"
command -v openssl >/dev/null || die "openssl is needed (apt install openssl, or yum install openssl)"

ask() { # ask VAR "Question" default
	local reply
	read -r -p "$2${3:+ [$3]}: " reply
	printf -v "$1" '%s' "${reply:-$3}"
}

# ---- 1. secrets ----
umask 077
if [ ! -f "$APP/config/tutoring.secret" ]; then
	say "Making the tutoring secret: $APP/config/tutoring.secret"
	openssl rand -hex 32 >"$APP/config/tutoring.secret"
fi
if [ ! -f "$APP/config/livekit-keys.yaml" ]; then
	say "Making LiveKit's key: $APP/config/livekit-keys.yaml"
	printf 'tutoring: %s\n' "$(openssl rand -hex 24)" >"$APP/config/livekit-keys.yaml"
fi
chmod 400 "$APP/config/tutoring.secret" "$APP/config/livekit-keys.yaml"
umask 022

# ---- 2. settings ----
if [ ! -f "$APP/config/tutor.env" ]; then
	main=${QP_BASE_URL#https://}
	echo
	echo "Tutoring needs its own subdomain, pointing at this server (an A record), for example tutor.${main#www.}:"
	ask tdomain "Tutoring subdomain" "${TUTOR_DOMAIN:-tutor.${main#www.}}"
	tdomain=${tdomain#https://}; tdomain=${tdomain#http://}; tdomain=${tdomain%%/*}
	[ -n "$tdomain" ] || die "a subdomain is needed"
	[ "$tdomain" != "$main" ] || die "tutoring needs a subdomain of its own, not the app's"
	echo
	echo "Ports on this server (only Nginx reaches the first two; the last two carry the video and must be open in the firewall):"
	ask tport "Tutoring service" "${TUTOR_PORT:-8090}"
	ask lkport "LiveKit" "${LIVEKIT_PORT:-7880}"
	ask lktcp "LiveKit media over TCP" "${LIVEKIT_TCP_PORT:-7881}"
	ask lkudp "LiveKit media over UDP" "${LIVEKIT_UDP_PORT:-7882}"
	for p in "$tport" "$lkport" "$lktcp" "$lkudp"; do
		[[ $p =~ ^[0-9]+$ ]] && [ "$p" -ge 1024 ] && [ "$p" -le 65535 ] || die "$p isn't a port from 1024 to 65535"
	done
	metrics=$((lkport + 909)) # 7880 → 8789: LiveKit's Prometheus metrics, on loopback only
	umask 077
	cat >"$APP/config/tutor.env" <<EOF
# Settings for tutoring (D-59), written by tutoring.sh. Root only: it holds
# the database password. Restart after changes: systemctl restart quiz-tutoring
TUTOR_DOMAIN="$tdomain"
TUTOR_LISTEN="127.0.0.1:$tport"
TUTOR_DATABASE_URL="$QP_DATABASE_URL"
TUTOR_PLATFORM_URL="http://127.0.0.1:$(app_port)"
TUTOR_PLATFORM_ORIGIN="$QP_BASE_URL"
TUTOR_RECORDINGS_DIR="$APP/data/recordings"
LIVEKIT_URL="http://127.0.0.1:$lkport"
LIVEKIT_PUBLIC_URL="wss://$tdomain"
LIVEKIT_API_KEY="tutoring"
LIVEKIT_METRICS_URL="http://127.0.0.1:$metrics/metrics"
GOMEMLIMIT="300MiB"
EOF
	cat >"$APP/config/livekit.yaml" <<EOF
# LiveKit (D-58), written by tutoring.sh. Its signalling (port $lkport) listens
# on loopback only, behind Nginx; media uses TCP $lktcp and UDP $lkudp, which the
# firewall must allow. Restart after changes: systemctl restart quiz-livekit
port: $lkport
rtc:
  tcp_port: $lktcp
  udp_port: $lkudp
  use_external_ip: true
logging:
  level: info
prometheus:
  port: $metrics
EOF
	umask 022
	chmod 600 "$APP/config/tutor.env" "$APP/config/livekit.yaml"
	say "Settings saved in $APP/config/tutor.env and $APP/config/livekit.yaml"
else
	say "Keeping the existing settings in $APP/config/tutor.env"
fi
tutor_env
set_env QP_TUTORING_URL "https://$TUTOR_DOMAIN"
mkdir -p "$APP/data/recordings"
[ -n "${QP_TEST:-}" ] || chown -R "$RUN_AS:$RUN_AS" "$APP/data/recordings"
chmod 700 "$APP/data/recordings"

# ---- 3. services ----
if [ -z "${QP_TEST:-}" ]; then
	say "Installing the quiz-livekit and quiz-tutoring services"
	cat >/etc/systemd/system/quiz-livekit.service <<EOF
[Unit]
Description=Classroom Quiz: LiveKit, tutoring's media server
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
DynamicUser=true
# Its key and settings reach it as credentials, never in the environment.
LoadCredential=keys:$APP/config/livekit-keys.yaml
LoadCredential=config:$APP/config/livekit.yaml
ExecStart=$APP/current/livekit-server --config %d/config --key-file %d/keys --bind 127.0.0.1
Restart=always
RestartSec=2
TimeoutStopSec=20
LimitNOFILE=65536
MemoryMax=1G
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
CapabilityBoundingSet=
AmbientCapabilities=

[Install]
WantedBy=multi-user.target
EOF
	cat >/etc/systemd/system/quiz-tutoring.service <<EOF
[Unit]
Description=Classroom Quiz: tutoring service
After=network-online.target postgresql.service pgsql.service quiz-livekit.service $SERVICE.service
Wants=network-online.target quiz-livekit.service

[Service]
Type=simple
User=$RUN_AS
Group=$RUN_AS
WorkingDirectory=$APP/data
EnvironmentFile=$APP/config/tutor.env
LoadCredential=tutoring_secret:$APP/config/tutoring.secret
LoadCredential=livekit_keys:$APP/config/livekit-keys.yaml
Environment=TUTOR_SECRET_FILE=%d/tutoring_secret
Environment=LIVEKIT_API_SECRET_FILE=%d/livekit_keys
ExecStart=$APP/current/tutor
Restart=always
RestartSec=2
TimeoutStopSec=20
# Its own limits, apart from the app's (TS-NFR-54).
MemoryMax=400M
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
	# The app's share of the secret, beside its own unit.
	mkdir -p "/etc/systemd/system/$SERVICE.service.d"
	cat >"/etc/systemd/system/$SERVICE.service.d/tutoring.conf" <<EOF
# Written by tutoring.sh: the secret shared with the tutoring service.
[Service]
LoadCredential=tutoring_secret:$APP/config/tutoring.secret
Environment=QP_TUTORING_SECRET_FILE=%d/tutoring_secret
EOF
	$SYSTEMCTL daemon-reload
	$SYSTEMCTL enable quiz-livekit quiz-tutoring >/dev/null
fi

# ---- 4. start ----
say "Restarting the app with tutoring on"
$SYSTEMCTL restart "$SERVICE"
healthy 90 || die "the app didn't start again. The log: journalctl -u $SERVICE -n 80"
say "Starting LiveKit and the tutoring service"
$SYSTEMCTL restart quiz-livekit
$SYSTEMCTL restart quiz-tutoring
tutor_healthy 60 || die "the tutoring service didn't start. The logs: journalctl -u quiz-tutoring -n 80, and journalctl -u quiz-livekit -n 80"

# ---- 5. Nginx ----
render_nginx
lk=$(livekit_ports)
echo
say "Tutoring is running. To finish (see the guide, \"Tutoring\"):"
echo "  1. Point $TUTOR_DOMAIN at this server (an A record), if it isn't yet."
echo "  2. aaPanel → Website → Add site: $TUTOR_DOMAIN (static, no PHP, no database). Its SSL tab: Let's Encrypt, Force HTTPS."
echo "     Its Config: delete aaPanel's static-file blocks, and paste $APP/nginx-tutor.conf below the SSL lines."
echo "  3. The app's site Config: paste $APP/nginx.conf again (it now allows $TUTOR_DOMAIN)."
echo "  4. Open TCP ${lk% *} and UDP ${lk#* } for the video: aaPanel → Security, and your cloud provider's firewall too."
echo "     Keep the other ports closed; only Nginx, on this server, uses them."
