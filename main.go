package main

import (
	"bufio"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/hpcloud/tail"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/promlog"
	"github.com/prometheus/common/promlog/flag"
	"github.com/prometheus/common/version"
	"gopkg.in/alecthomas/kingpin.v2"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
)

type Collector struct {
	logger  log.Logger
	mainlog *string
}

func (c Collector) TailMainlog() bool {
	level.Info(c.logger).Log("msg", "Opening mainlog")
	t, err := tail.TailFile(*c.mainlog, tail.Config{
		Follow:    true,
		MustExist: true,
	})
	if err != nil {
		level.Error(c.logger).Log(err)
		return false
	}

	for line := range t.Lines {
		parts := strings.SplitN(line.Text, " ", 5)
		if len(parts) < 3 {
			continue
		}
		op := parts[3]
		switch op {
		case "<=":
			totalMessages.With(prometheus.Labels{"flag": "arrived"}).Inc()
		case "(=":
			totalMessages.With(prometheus.Labels{"flag": "fakereject"}).Inc()
		case "=>":
			totalMessages.With(prometheus.Labels{"flag": "delivered"}).Inc()
		case "->":
			totalMessages.With(prometheus.Labels{"flag": "additional"}).Inc()
		case ">>":
			totalMessages.With(prometheus.Labels{"flag": "cutthrough"}).Inc()
		case "*>":
			totalMessages.With(prometheus.Labels{"flag": "suppressed"}).Inc()
		case "**":
			totalMessages.With(prometheus.Labels{"flag": "failed"}).Inc()
		case "==":
			totalMessages.With(prometheus.Labels{"flag": "deferred"}).Inc()
		case "Completed":
			messagesCompleted.Inc()
		}
	}
	return true
}

func (c Collector) QueueSize() float64 {
	level.Debug(c.logger).Log("msg", "Running exim -bpc")
	out, err := exec.Command("exim", "-bpc").Output()
	if err != nil {
		level.Error(c.logger).Log("msg", err)
		return -1
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil {
		level.Error(c.logger).Log("msg", err)
		return -1
	}
	return value
}

func (c Collector) ProcessStates() {
	level.Debug(c.logger).Log("msg", "Running exiwhat")
	cmd := exec.Command("exiwhat")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		level.Error(c.logger).Log("msg", err)
		return
	}
	if err := cmd.Start(); err != nil {
		level.Error(c.logger).Log("msg", err)
		return
	}
	s := bufio.NewScanner(stdout)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		parts := strings.SplitN(line, " ", 3)
		if len(parts) < 2 {
			continue
		}
		op := parts[1]
		switch true {
		case strings.HasPrefix(op, "daemon"):
			processStates.With(prometheus.Labels{"state": "daemon"}).Inc()
		default:
			processStates.With(prometheus.Labels{"state": op}).Inc()
		}
	}
}

var (
	totalMessages = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "exim_messages_total",
			Help: "Total messages",
		},
		[]string{"flag"},
	)
	messagesCompleted = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "exim_messages_completed_total",
			Help: "Total messages completed by exim",
		},
	)
	processStates = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "exim_process_states",
			Help: "Total exim processes by type (exiwhat)",
		},
		[]string{"state"},
	)
)

func main() {
	var (
		mainlog = kingpin.Flag("exim.mainlog", "Exim main logger file.").Default("mainlog").String()
		//paniclog       = kingpin.Flag("exim.paniclog", "Exim main logger file.").Default("paniclog").String()
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
	c := Collector{logger, mainlog}
	go c.TailMainlog()
	go c.ProcessStates()

	promauto.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "exim_queue_size",
		Help: "Total messages currently queued by exim (exim -bpc)",
	}, c.QueueSize)

	http.Handle(*metricsPath, promhttp.Handler())
	level.Info(logger).Log("msg", "Listening", "address", listenAddress)
	level.Error(logger).Log(http.ListenAndServe(*listenAddress, nil))

}
