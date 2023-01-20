//go:build !systemd
// +build !systemd

package main

import (
	"github.com/go-kit/kit/log/level"
	"github.com/nxadm/tail"
	"log/syslog"
	"os"
)

func (e *Exporter) JournalTail(identifier string, priority syslog.Priority) chan *tail.Line {
	_ = identifier
	_ = priority
	_ = level.Error(e.logger).Log("msg", "Not compiled with systemd support. (-tags systemd)")
	os.Exit(1)
	return make(chan *tail.Line)
}
