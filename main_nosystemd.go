// +build !systemd

package main

import (
	"github.com/hpcloud/tail"
	"log"
	"log/syslog"
)

func (e *Exporter) JournalTail(identifier string, priority syslog.Priority) chan *tail.Line {
	_ = identifier
	_ = priority
	log.Fatal("Not compiled with systemd support. (-tags systemd)")
	return make(chan *tail.Line)
}
