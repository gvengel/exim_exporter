package main

import (
	"bufio"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/hpcloud/tail"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/promlog"
	"github.com/prometheus/common/promlog/flag"
	"github.com/prometheus/common/version"
	"gopkg.in/alecthomas/kingpin.v2"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

var (
	eximUp = prometheus.NewDesc(
		prometheus.BuildFQName("exim", "", "up"),
		"Whether or not the main exim daemon is running",
		nil, nil,
	)
	eximQueue = prometheus.NewDesc(
		prometheus.BuildFQName("exim", "", "queue"),
		"Number of messages currently in queue `exim -bpc`",
		nil, nil,
	)
	eximProcesses = prometheus.NewDesc(
		prometheus.BuildFQName("exim", "daemon", "processes"),
		"Number of running exim process broken down by state (delivering, handling, etc) `exiwhat`",
		[]string{"state"}, nil,
	)
	eximMessages = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "exim_messages_total",
			Help: "Total number of logged messages broken down by flag (delivered, deferred, etc)",
		},
		[]string{"flag"},
	)
	eximReject = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "exim_reject_total",
			Help: "Total number of logged reject messages",
		},
	)
	eximPanic = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "exim_panic_total",
			Help: "Total number of logged panic messages",
		},
	)
)

type Exporter struct {
	mainlog   *string
	rejectlog *string
	paniclog  *string
	logger    log.Logger
}

func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	ch <- eximUp
	ch <- eximProcesses
}

func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	states := e.ProcessStates()
	queue := e.QueueSize()
	up := float64(0)
	if _, ok := states["daemon"]; ok {
		up = 1
	}
	ch <- prometheus.MustNewConstMetric(eximUp, prometheus.GaugeValue, up)
	for label, value := range states {
		ch <- prometheus.MustNewConstMetric(eximProcesses, prometheus.GaugeValue, value, label)
	}
	ch <- prometheus.MustNewConstMetric(eximQueue, prometheus.GaugeValue, queue)
}

func (e *Exporter) ProcessStates() map[string]float64 {
	level.Debug(e.logger).Log("msg", "Running exiwhat")
	var states = map[string]float64{}
	cmd := exec.Command("exiwhat")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		level.Error(e.logger).Log("msg", err)
		return states
	}
	if err := cmd.Start(); err != nil {
		level.Error(e.logger).Log("msg", err)
		return states
	}
	s := bufio.NewScanner(stdout)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		parts := strings.SplitN(line, " ", 3)
		if len(parts) < 2 {
			continue
		}
		op := parts[1]
		if strings.HasPrefix(op, "daemon") {
			op = "daemon"
		}
		states[op] += 1
	}
	return states
}

func (e *Exporter) QueueSize() float64 {
	level.Debug(e.logger).Log("msg", "Running exim -bpc")
	out, err := exec.Command("exim", "-bpc").Output()
	if err != nil {
		level.Error(e.logger).Log("msg", err)
		return -1
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil {
		level.Error(e.logger).Log("msg", err)
		return -1
	}
	return value
}

func (e *Exporter) Start() {
	go e.TailMainLog()
	go e.TailRejectLog()
	go e.TailPanicLog()
}

func (e *Exporter) Tail(filename string) *tail.Tail {
	level.Info(e.logger).Log("msg", "Opening mainlog")
	t, err := tail.TailFile(filename, tail.Config{
		Follow:    true,
		MustExist: true,
	})
	if err != nil {
		level.Error(e.logger).Log("msg", "Unable to open log", "err", err)
		os.Exit(1)
	}
	return t
}

func (e *Exporter) TailMainLog() {
	t := e.Tail(*e.mainlog)
	for line := range t.Lines {
		level.Debug(e.logger).Log("msg", line.Text)
		parts := strings.SplitN(line.Text, " ", 5)
		if len(parts) < 3 {
			continue
		}
		op := parts[3]
		switch op {
		case "<=":
			eximMessages.With(prometheus.Labels{"flag": "arrived"}).Inc()
		case "(=":
			eximMessages.With(prometheus.Labels{"flag": "fakereject"}).Inc()
		case "=>":
			eximMessages.With(prometheus.Labels{"flag": "delivered"}).Inc()
		case "->":
			eximMessages.With(prometheus.Labels{"flag": "additional"}).Inc()
		case ">>":
			eximMessages.With(prometheus.Labels{"flag": "cutthrough"}).Inc()
		case "*>":
			eximMessages.With(prometheus.Labels{"flag": "suppressed"}).Inc()
		case "**":
			eximMessages.With(prometheus.Labels{"flag": "failed"}).Inc()
		case "==":
			eximMessages.With(prometheus.Labels{"flag": "deferred"}).Inc()
		case "Completed":
			eximMessages.With(prometheus.Labels{"flag": "completed"}).Inc()
		}
	}
}

func (e *Exporter) TailRejectLog() {
	t := e.Tail(*e.rejectlog)
	for line := range t.Lines {
		level.Debug(e.logger).Log("msg", line.Text)
		eximReject.Inc()
	}
}

func (e *Exporter) TailPanicLog() {
	t := e.Tail(*e.paniclog)
	for line := range t.Lines {
		level.Debug(e.logger).Log("msg", line.Text)
		eximPanic.Inc()
	}
}

func init() {
	prometheus.MustRegister(version.NewCollector("exim_exporter"))
	prometheus.MustRegister(eximMessages)
	prometheus.MustRegister(eximReject)
	prometheus.MustRegister(eximPanic)
}

func main() {
	var (
		mainlog       = kingpin.Flag("exim.mainlog", "Path to Exim main log file.").Default("/var/log/mainlog").String()
		rejectlog     = kingpin.Flag("exim.rejectlog", "Path to Exim reject log file.").Default("/var/log/exim4/rejectlog").String()
		paniclog      = kingpin.Flag("exim.paniclog", "Path to Exim panic log file.").Default("/var/log/paniclog").String()
		listenAddress = kingpin.Flag("web.listen-address", "Address to listen on for web interface and telemetry.").Default(":9350").String()
		metricsPath   = kingpin.Flag("web.telemetry-path", "Path under which to expose metrics.").Default("/metrics").String()
	)
	promlogConfig := &promlog.Config{}
	flag.AddFlags(kingpin.CommandLine, promlogConfig)
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()
	logger := promlog.New(promlogConfig)

	level.Info(logger).Log("msg", "Starting exim exporter", "version", version.Info())
	level.Info(logger).Log("msg", "Build context", "context", version.BuildContext())

	// TODO: test for queue_list_requires_admin = false
	exporter := &Exporter{
		mainlog,
		rejectlog,
		paniclog,
		logger,
	}
	exporter.Start()
	prometheus.MustRegister(exporter)

	http.Handle(*metricsPath, promhttp.Handler())
	level.Info(logger).Log("msg", "Listening", "address", listenAddress)
	level.Error(logger).Log(http.ListenAndServe(*listenAddress, nil))

}
