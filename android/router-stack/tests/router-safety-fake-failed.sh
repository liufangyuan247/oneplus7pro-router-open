#!/system/bin/sh

case "${1:-}" in
  daemon) exit 23 ;;
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
