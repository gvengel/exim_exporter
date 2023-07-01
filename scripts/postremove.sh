#!/bin/sh
set -e
# Automatically added by dh_installinit/11.1.6ubuntu2
if [ "$1" = "purge" ] ; then
	update-rc.d prometheus-exim-exporter remove >/dev/null
fi


# In case this system is running systemd, we make systemd reload the unit files
# to pick up changes.
if [ -d /run/systemd/system ] ; then
	systemctl --system daemon-reload >/dev/null || true
fi
# End automatically added section
# Automatically added by dh_systemd_start/11.1.6ubuntu2
if [ -d /run/systemd/system ]; then
	systemctl --system daemon-reload >/dev/null || true
fi
# End automatically added section
# Automatically added by dh_systemd_enable/11.1.6ubuntu2
if [ "$1" = "remove" ]; then
	if [ -x "/usr/bin/deb-systemd-helper" ]; then
		deb-systemd-helper mask 'prometheus-exim-exporter.service' >/dev/null || true
	fi
fi

if [ "$1" = "purge" ]; then
	if [ -x "/usr/bin/deb-systemd-helper" ]; then
		deb-systemd-helper purge 'prometheus-exim-exporter.service' >/dev/null || true
		deb-systemd-helper unmask 'prometheus-exim-exporter.service' >/dev/null || true
	fi
fi
# End automatically added section
