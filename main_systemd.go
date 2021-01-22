// +build systemd

package main

import (
	"errors"
	"fmt"
	"github.com/coreos/go-systemd/sdjournal"
	"github.com/go-kit/kit/log/level"
	"github.com/hpcloud/tail"
	"log/syslog"
	"os"
	"time"
)

func (e *Exporter) JournalTail(identifier string, priority syslog.Priority) chan *tail.Line {
	j, err := sdjournal.NewJournal()
	if err != nil {
		level.Error(e.logger).Log("msg", "Unable to open journal", "err", err)
		os.Exit(1)
	}
	if err := j.AddMatch(fmt.Sprintf("PRIORITY=%d", priority)); err != nil {
		level.Error(e.logger).Log("msg", "Could not setup priority journal match", "err", err)
		os.Exit(1)
	}
	if err := j.AddMatch(fmt.Sprintf("SYSLOG_IDENTIFIER=%s", identifier)); err != nil {
		level.Error(e.logger).Log("msg", "Could not setup syslog identifier journal match", "err", err)
		os.Exit(1)
	}
	if err := j.SeekTail(); err != nil {
		level.Error(e.logger).Log("msg", "Could not seek to journal tail", "err", err)
		os.Exit(1)
	}
	// Apparently we need to go one back to avoid getting older entries from before we start.
	// This looks like a bug in the library.
	if _, err := j.Previous(); err != nil {
		level.Error(e.logger).Log("msg", "Could not advance one journal entry", "err", err)
		os.Exit(1)
	}

	lines := make(chan *tail.Line)
	go func() {
		defer func() {
			close(lines)
			if err := j.Close(); err != nil {
				level.Error(e.logger).Log("msg", "Could not close journal", "err", err)
			}
		}()

		for {
			if ret, err := j.Next(); err != nil {
				lines <- &tail.Line{
					Err: fmt.Errorf("could not call Next(): %w", err),
				}
				return
			} else if ret == 0 {
				// End of journal, wait for a new entry to be written.
				for r := j.Wait(sdjournal.IndefiniteWait); r == sdjournal.SD_JOURNAL_NOP; {
				}
				continue
			}
			je, err := j.GetEntry()
			if err != nil {
				lines <- &tail.Line{
					Err: fmt.Errorf("could not GetEntry(): %w", err),
				}
				continue
			}
			text, ok := je.Fields["MESSAGE"]
			if !ok {
				lines <- &tail.Line{
					Err: errors.New("got journal entry without MESSAGE set"),
				}
				continue
			}
			lines <- &tail.Line{
				Text: text,
				Time: time.Unix(int64(je.RealtimeTimestamp/10e6), int64(je.RealtimeTimestamp%10e6*1000)),
			}
		}
	}()
	return lines
}
