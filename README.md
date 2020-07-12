# Exim Exporter for Prometheus 
[![Build Status](https://travis-ci.com/gvengel/exim_exporter.svg?token=qhTuSsVmWS1s5LkEYqfN&branch=master)](https://travis-ci.com/gvengel/exim_exporter)
[![Go Report Card](https://goreportcard.com/badge/github.com/gvengel/exim_exporter)](https://goreportcard.com/report/github.com/gvengel/exim_exporter) 

This prometheus exporter monitors the [exim4](https://www.exim.org/) mail transport server. 
Stats are collected by tailing exim's log files, counting messages queued on disk, 
and observing running exim processes.

## Installing

Download and run the latest binary from the [releases tab](https://github.com/gvengel/exim_exporter/releases/latest). 

```shell script
.\exim_exporter 
```

The exporter will need read access to exim's log and spool directories. 
Therefore, it is recommended to run as the same user as the exim server.

Alternately, a simple Debian package is provided which installs the exporter as a service.

```shell script
dpkg -i prometheus-exim-exporter*.deb
```

## Usage

The default settings are intend for Debian/Ubuntu. Other distributions may configure exim with different system paths, 
and the exporter will need to be configured to match.

See `--help` for more details. 
Note, command line arguments can also be set via environment variable. e.g `--exim.mainlog` -> `EXIM_MAINLOG`.

## Building

```sh
make
```

## Metrics

See example metrics in [tests](https://github.com/gvengel/exim_exporter/blob/master/test/update.metrics).

### exim_messages_total

This stat is calculated by tailing the exim mainlog and returning a counter with labels for each
[log message flag](https://www.exim.org/exim-html-current/doc/html/spec_html/ch-log_files.html#SECID250). 
An additional label is added for messages marked as completed.

| Prom Label | Exim Flag |
|------------|-----------|
| arrived    | <=        |
| fakereject | (=        |
| delivered  | =>        |
| additional | ->        |
| cutthrough | \>\>      |
| suppressed | *>        |
| failed     | **        |
| deferred   | ==        |
| completed  | Completed |
		
### exim_reject_total and exim_panic_total 

These stats are calculated by tailing the rejectlog and paniclog, returning counter for the number of lines in each.

### exim_processes

This metric returns the number of running exim process, labeled by process state.
The state is detected by parsing the process's command line and looking for know arguments.
While this method doesn't provide the same detail as `exiwhat`, that tool is 
[contraindicated for using in monitoring](https://www.exim.org/exim-html-current/doc/html/spec_html/ch-exim_utilities.html#SECTfinoutwha).

| Prom Label | Exim State |
|------------|------------|
| daemon     | parent pid |
| delivering | exim -Mc   |
| handling   | exim -bd   |
| running    | exim -qG   |
| other      | other      | 

### exim_queue

This metric reports the equivalent of `exim -bpc`. Note, the value is calculated by independently parsing the queue, not forking to exim.

### exim_up

Whether the main exim daemon is running.
