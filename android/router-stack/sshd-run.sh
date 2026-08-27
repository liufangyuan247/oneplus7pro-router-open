#!/data/data/com.termux/files/usr/bin/sh
exec 2>&1
export HOME='/data/data/com.termux/files/home'
export PREFIX='/data/data/com.termux/files/usr'
export PATH='/data/data/com.termux/files/usr/bin'
export TMPDIR='/data/data/com.termux/files/usr/tmp'
exec '/data/data/com.termux/files/usr/bin/sshd' -D -e
