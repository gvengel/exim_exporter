# Exim Exporter for Prometheus [![Build Status](https://travis-ci.com/gvengel/exim_exporter.svg?token=qhTuSsVmWS1s5LkEYqfN&branch=master)](https://travis-ci.com/gvengel/exim_exporter)

This prometheus exporter monitors the [exim4](https://www.exim.org/) mail transport server. 
Stats are collected by tailing exim's log files, counting messages queued on disk, 
and observing running exim processes.

## Installing

Download and run the latest binary from the [releases tab](https://github.com/gvengel/exim_exporter/releases/latest). 

```shell script
.\exim_exporter 
```

The exporter will need read access to exim's log and spool directories. 
Therefor, it is recommended to run as the same user as the exim server.

Alternately, a simple Debian package is provided which installs the exporter as a service.

```shell script
dpkg -i prometheus-exim-exporter*.deb
```

## Usage

The default settings are intend for Debian/Ubuntu. Other distributions may configure exim with different system paths, 
and the exporter will need to be configured to match.

See `--help` for more detail.

## Building

```sh
make
```

