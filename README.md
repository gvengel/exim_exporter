# Exim Exporter for Prometheus

[![build](https://github.com/gvengel/exim_exporter/actions/workflows/build.yml/badge.svg)](https://github.com/gvengel/exim_exporter/actions/workflows/build.yml)
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

### `exim_up`

Whether the main exim daemon is running.

### `exim_queue`

This metric reports the equivalent of `exim -bpc`. Note, the value is calculated by independently parsing the queue, not
forking to exim.

### `exim_queue_frozen`

This metric reports the number of frozen messages in the queue. To retrieve queued message states, the exporter must
read the header file for each message. This is expensive for very large queues (thousands or millions of messages), so
the `--queue.read-timeout` limits the amount of time the exporter will spend scanning message headers. If the timeout is
exceeded, the number of scanned messages is recorded, and the exporter will not attempt to read message headers again
until the queue has dropped to below 80% of the value.

### `exim_queue_read_timeout_errors_total`

The total number of timeout errors encountered while reading message states from the queue. e.g. while calculating the
`exim_queue_frozen` metric.

### `exim_processes`

This metric returns the number of running exim process, labeled by process state.
The state is detected by parsing the process's command line and looking for known arguments.
While this method doesn't provide the same detail as `exiwhat`, that tool is
[contraindicated for use in monitoring](https://www.exim.org/exim-html-current/doc/html/spec_html/ch-exim_utilities.html#SECTfinoutwha).

| Prom Label | Exim State |
|------------|------------|
| daemon     | parent pid |
| delivering | exim -Mc   |
| handling   | exim -bd   |
| running    | exim -qG   |
| other      | other      | 

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

### `exim_message_errors_total`

Number of logged messages broken down by error status code (451, 550, etc) and enhanced error status code (4.7.0, 5.7.1,
etc). The status code format can vary by remote MTA, so the exporter may not detect all status correctly.

### `exim_reject_total` and `exim_panic_total `

These stats are calculated by tailing the rejectlog and paniclog, returning counter for the number of lines in each.

## `exim_log_read_errors`

This metrics reports any failures encountered while tailing the logs.

## Docker

Docker images are available on [docker hub](https://hub.docker.com/r/gvengel/exim_exporter).

To enable full functionality the exporter needs access to exim's log files, spool directory, and process list.
For example, if you were running your MTA in a container named `exim4`, usage might look something like:

```
docker run 
  -p 9636:9636 \
  -v /var/log/exim4:/var/log/exim4 \
  -v /var/spool/exim4:/var/spool/exim4 \
  --pid container:exim4 \
  --name exim_exporter \
  gvengel/exim_exporter
```

Also see the provided [docker-compose](examples/docker-compose.yml) example.