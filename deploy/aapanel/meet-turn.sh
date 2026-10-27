#!/usr/bin/env bash
# Sets up the relay for meetings (D-93). Meetings send video straight
# between browsers; on mobile data and some school or office networks that
# fails ("Can't connect"), and coturn on this server carries the video
# instead. The app hands each person relay credentials that expire after 6
# hours, made with a secret the two share. Safe to run again.
#
# Usage: sudo bash /www/quiz-app/meet-turn.sh
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=lib.sh
. "$here/lib.sh"
need_root "$@"
load_env

ask() { # ask VAR "Question" default
	local reply
	read -r -p "$2${3:+ [$3]}: " reply
	printf -v "$1" '%s' "${reply:-$3}"
}

domain=${QP_BASE_URL#https://}
domain=${domain%%/*}
ip=$(curl -4 -fsS --max-time 5 https://api.ipify.org 2>/dev/null || hostname -I | awk '{print $1}')
echo "The relay is reached by this server's name or address, on these ports (open them in the firewall):"
ask name "Relay host name" "$domain"
ask pubip "This server's public IPv4 address" "$ip"
ask port "Relay port (UDP and TCP)" "3478"
ask lo "Relayed video, first UDP port" "49160"
ask hi "Relayed video, last UDP port" "49200"
for p in "$port" "$lo" "$hi"; do
	[[ $p =~ ^[0-9]+$ ]] && [ "$p" -ge 1024 ] && [ "$p" -le 65535 ] || die "$p isn't a port from 1024 to 65535"
done
[ "$lo" -lt "$hi" ] || die "the first relay port must be below the last"

# ---- coturn ----
if [ -z "${QP_TEST:-}" ] && ! command -v turnserver >/dev/null; then
	say "Installing coturn"
	if command -v apt-get >/dev/null; then
		DEBIAN_FRONTEND=noninteractive apt-get install -y coturn >/dev/null
	elif command -v dnf >/dev/null; then
		dnf install -y coturn >/dev/null
	elif command -v yum >/dev/null; then
		yum install -y coturn >/dev/null
	else
		die "install coturn with your system's package manager, then run this again"
	fi
fi
if [ -z "${QP_TEST:-}" ] && ! pgrep -x turnserver >/dev/null; then
	for proto in udp tcp; do
		port_free "$port" "$proto" || die "port $port ($proto) is used by another program ($(port_user "$port" "$proto")): run this again and choose another"
	done
fi

umask 077
[ -s "$APP/config/meet-turn.secret" ] || openssl rand -hex 32 >"$APP/config/meet-turn.secret"
chmod 400 "$APP/config/meet-turn.secret"
umask 022
conf=${QP_TURN_CONF:-/etc/turnserver.conf}
[ -n "${QP_TURN_CONF:-}" ] || [ ! -d /etc/coturn ] || conf=/etc/coturn/turnserver.conf
say "Writing $conf"
cat >"$conf" <<EOF
# Written by meet-turn.sh (D-93): the relay for the quiz app's meetings.
listening-port=$port
fingerprint
use-auth-secret
static-auth-secret=$(cat "$APP/config/meet-turn.secret")
realm=$name
server-name=$name
external-ip=$pubip
min-port=$lo
max-port=$hi
no-cli
no-multicast-peers
no-tlsv1
no-tlsv1_1
# Never relay into this server or private networks.
denied-peer-ip=0.0.0.0-0.255.255.255
denied-peer-ip=10.0.0.0-10.255.255.255
denied-peer-ip=100.64.0.0-100.127.255.255
denied-peer-ip=127.0.0.0-127.255.255.255
denied-peer-ip=169.254.0.0-169.254.255.255
denied-peer-ip=172.16.0.0-172.31.255.255
denied-peer-ip=192.168.0.0-192.168.255.255
denied-peer-ip=::1
denied-peer-ip=fc00::-fdff:ffff:ffff:ffff:ffff:ffff:ffff:ffff
EOF
chmod 640 "$conf"
getent group turnserver >/dev/null && chown root:turnserver "$conf"
getent group coturn >/dev/null && chown root:coturn "$conf"
[ ! -f /etc/default/coturn ] || sed -i 's/^#\?TURNSERVER_ENABLED=.*/TURNSERVER_ENABLED=1/' /etc/default/coturn
$SYSTEMCTL enable coturn >/dev/null 2>&1 || true
$SYSTEMCTL restart coturn
sleep 1
$SYSTEMCTL is-active --quiet coturn || die "coturn didn't start. Its log: journalctl -u coturn -n 40"

# ---- the app ----
if [ -z "${QP_TEST:-}" ]; then
	mkdir -p "/etc/systemd/system/$SERVICE.service.d"
	cat >"/etc/systemd/system/$SERVICE.service.d/meet-turn.conf" <<EOF
# Written by meet-turn.sh: the secret shared with coturn (D-93).
[Service]
LoadCredential=meet_turn:$APP/config/meet-turn.secret
Environment=QP_MEET_TURN_SECRET_FILE=%d/meet_turn
EOF
	$SYSTEMCTL daemon-reload
fi
unset_env QP_MEET_TURN_SECRET_FILE
set_env QP_MEET_TURN_URLS "turn:$name:$port?transport=udp,turn:$name:$port?transport=tcp"
say "Restarting the app"
$SYSTEMCTL restart "$SERVICE"
healthy 90 || die "the app didn't start again. The log: journalctl -u $SERVICE -n 80"
echo
say "The relay is running. To finish:"
echo "  1. Open UDP and TCP $port, and UDP $lo-$hi (type a plain hyphen), in aaPanel → Security and in your provider's firewall."
echo "  2. People already in a meeting leave and join again; new joiners get the relay by themselves."
