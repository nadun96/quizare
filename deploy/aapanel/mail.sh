#!/usr/bin/env bash
# Sets up email (D-93): password resets, verification, tutoring reminders and
# notices go out through an SMTP relay (Brevo, Mailgun, Gmail with an app
# password, …). Safe to run again: it replaces the earlier settings.
#
# The password is kept in a root-only file and handed to the app by systemd
# (LoadCredential), as the master key is; the app's user can't read the
# config folder, so a path there in quiz.env would stop the app.
#
# Usage: sudo bash /www/quiz-app/mail.sh
#        sudo bash /www/quiz-app/mail.sh off     (back to writing emails to the log)
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=lib.sh
. "$here/lib.sh"
need_root "$@"
load_env
dropin="/etc/systemd/system/$SERVICE.service.d/smtp.conf"

if [ "${1:-}" = off ]; then
	for k in QP_SMTP_ADDR QP_SMTP_FROM QP_SMTP_USER QP_SMTP_PASSWORD_FILE; do unset_env "$k"; done
	rm -f "$dropin" "$APP/config/smtp_password"
	$SYSTEMCTL daemon-reload
	$SYSTEMCTL restart "$SERVICE"
	healthy 90 || die "the app didn't start again. The log: journalctl -u $SERVICE -n 80"
	say "Email is off: messages are written to the app's log instead"
	exit 0
fi

ask() { # ask VAR "Question" default
	local reply
	read -r -p "$2${3:+ [$3]}: " reply
	printf -v "$1" '%s' "${reply:-$3}"
}

domain=${QP_BASE_URL#https://}
echo "Your mail provider's SMTP details. Port 587 (STARTTLS) is needed: 465 isn't supported."
addr=${QP_SMTP_ADDR:-}
ask host "SMTP server" "${addr%:*}"
[ -n "$host" ] || die "an SMTP server is needed, for example smtp-relay.brevo.com"
ask port "Port" "587"
[ "$port" != 465 ] || die "port 465 (direct TLS) isn't supported: use 587"
ask user "Login (user name)" "${QP_SMTP_USER:-}"
ask from "Send as" "${QP_SMTP_FROM:-no-reply@${domain#www.}}"
[[ $from == *@* ]] || die "\"$from\" isn't an email address"
read -r -s -p "Password (or API key; Enter keeps the saved one): " pass
echo

if command -v timeout >/dev/null && ! timeout 8 bash -c "</dev/tcp/$host/$port" 2>/dev/null; then
	die "can't reach $host on port $port from this server. Check the name, or ask your hosting provider whether they block outgoing mail ports"
fi

umask 077
if [ -n "$pass" ]; then
	printf '%s' "$pass" >"$APP/config/smtp_password"
elif [ ! -s "$APP/config/smtp_password" ] && [ -n "$user" ]; then
	die "a password is needed"
fi
umask 022
[ ! -f "$APP/config/smtp_password" ] || chmod 400 "$APP/config/smtp_password"
unset pass

unset_env QP_SMTP_PASSWORD_FILE # set by the credential below, never here
set_env QP_SMTP_ADDR "$host:$port"
set_env QP_SMTP_FROM "$from"
if [ -n "$user" ]; then set_env QP_SMTP_USER "$user"; else unset_env QP_SMTP_USER; fi

if [ -z "${QP_TEST:-}" ]; then
	mkdir -p "$(dirname "$dropin")"
	if [ -s "$APP/config/smtp_password" ]; then
		cat >"$dropin" <<EOF
# Written by mail.sh: the SMTP password, as a credential (D-93).
[Service]
LoadCredential=smtp_password:$APP/config/smtp_password
Environment=QP_SMTP_PASSWORD_FILE=%d/smtp_password
EOF
	else
		rm -f "$dropin"
	fi
	$SYSTEMCTL daemon-reload
fi
say "Restarting the app"
$SYSTEMCTL restart "$SERVICE"
healthy 90 || die "the app didn't start again. The log: journalctl -u $SERVICE -n 80"
say "Email is on, sent through $host:$port as $from."
echo "  Try it: Forgot password on the login page, with an address you can check."
echo "  Watch it go: journalctl -u $SERVICE -f -o cat | grep -i -E 'mail|smtp'"
echo "  Not arriving? Look in spam, and add your provider's SPF and DKIM records for ${from#*@}."
