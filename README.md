# Exim Exporter for Prometheus 
[![Build Status](https://travis-ci.com/gvengel/exim_exporter.svg?token=qhTuSsVmWS1s5LkEYqfN&branch=master)](https://travis-ci.com/gvengel/exim_exporter)
[![Go Report Card](https://goreportcard.com/badge/github.com/gvengel/exim_exporter)](https://goreportcard.com/report/github.com/gvengel/exim_exporter) 

This prometheus exporter monitors the [exim4](https://www.exim.org/) mail transport server. 
Stats are collected by tailing exim's log files, counting messages queued on disk, 
and observing running exim processes.

## Installing

Download and run the latest binary from the [releases tab](https://github.com/gvengel/exim_exporter/releases/latest). 

```shell script
./exim_exporter
```

The exporter will need read access to exim's logs and its spool directory.
Therefore, it is recommended to run as the same user as the exim server.

Alternately, a simple Debian package is provided which installs the exporter as a service.

```shell script
dpkg -i prometheus-exim-exporter*.deb
```

## Usage

By default, the exporter serves on port `9636` at `/metrics`. 

The exporter has two modes:

1. The default mode is to process the log files appended to by exim by tailing
   them. The default is good for Debian/Ubuntu servers which store the logs in
   `/var/log/exim4`. Running in this mode on other distributions may require
   manually configuring the paths.
2. The second mode utilizes the systemd journal, tailing it for any log lines
   sent with the syslog identifier `exim` (configurable). This mode can be
   enabled using `--exim.use-journald`.

In both modes the exporter will additionally poll your spool directory to
determine the length of the mail queue.

See `--help` for more details. Command line arguments can also be set via
environment variable. e.g `--exim.mainlog` -> `EXIM_MAINLOG`.

## Building

```sh
make
```

## Metrics

See example metrics in [tests](https://github.com/gvengel/exim_exporter/blob/master/test/update.metrics).

### `exim_messages_total`

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
		
### `exim_reject_total` and `exim_panic_total `

These stats are calculated by tailing the rejectlog and paniclog, returning counter for the number of lines in each.

### `exim_processes`

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

### `exim_queue`

This metric reports the equivalent of `exim -bpc`. Note, the value is calculated by independently parsing the queue, not forking to exim.

### `exim_up`

Whether the main exim daemon is running.

## `exim_log_read_errors`

This metrics reports any failures encountered while tailing the logs.

## Docker

Docker images are available on [docker hub](https://hub.docker.com/r/gvengel/exim_exporter). 

For full functionality the exporter needs access to exim's log files, spool directory, and process list. 
If you were running your MTA in a container named `exim4`, usage might look something like:

```
docker run 
  -p 9636:9636 \
  -v /var/log/exim4:/var/log/exim4 \
  -v /var/spool/exim4:/var/spool/exim4 \
  --pid container:exim4 \
  --name exim_exporter \
  gvengel/exim_exporter
```

Also see the provided [docker-compose](docker-compose.yml) example.