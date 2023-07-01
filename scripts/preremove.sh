#!/bin/sh
set -e
# Automatically added by dh_installinit/11.1.6ubuntu2
if [ -x "/etc/init.d/prometheus-exim-exporter" ] && [ "$1" = remove ]; then
	invoke-rc.d prometheus-exim-exporter stop || exit 1
fi
# End automatically added section
# Automatically added by dh_systemd_start/11.1.6ubuntu2
if [ -d /run/systemd/system ] && [ "$1" = remove ]; then
	deb-systemd-invoke stop 'prometheus-exim-exporter.service' >/dev/null || true
fi
# End automatically added section
