#!/system/bin/sh

BASE="${ROUTER_SAFETY_BASE}"

case "${1:-}" in
  daemon)
    echo $$ > "$BASE/manager.pid"
    trap 'rm -f "$BASE/manager.pid"; exit 0' INT TERM EXIT
    while :; do sleep 1; done
    ;;
  status)
    echo armed=yes
    echo active=no
    echo 'wan=missing carrier=0'
    echo 'lan=missing carrier=0'
    echo ppp=down
    echo dnsmasq=down
    ;;
  validate) echo VALID ;;
  *) exit 2 ;;
esac
