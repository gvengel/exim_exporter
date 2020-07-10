package main

import (
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/hpcloud/tail"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/promlog"
	"github.com/prometheus/common/promlog/flag"
	"github.com/prometheus/common/version"
	"github.com/shirou/gopsutil/process"
	"gopkg.in/alecthomas/kingpin.v2"
	"io"
	stdlog "log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const BASE62 = "0123456789aAbBcCdDeEfFgGhHiIjJkKlLmMnNoOpPqQrRsStTuUvVwWxXyYzZ"

var (
	eximUp = prometheus.NewDesc(
		prometheus.BuildFQName("exim", "", "up"),
		"Whether or not the main exim daemon is running",
		nil, nil,
	)
	eximQueue = prometheus.NewDesc(
		prometheus.BuildFQName("exim", "", "queue"),
		"Number of messages currently in queue",
		nil, nil,
	)
	eximProcesses = prometheus.NewDesc(
		prometheus.BuildFQName("exim", "daemon", "processes"),
		"Number of running exim process broken down by state (delivering, handling, etc)",
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

var processFlags = map[string]string{
	"-Mc": "delivering",
	"-bd": "handling",
	"-qG": "running",
}

type Process struct {
	cmdline []string
	ppid    int32
}

// map globals we can override in tests
var (
	getProcesses = func() ([]*Process, error) {
		processes, err := process.Processes()
		if err != nil {
			return nil, err
		}
		result := make([]*Process, 0)
		for _, p := range processes {
			cmdline, err := p.CmdlineSlice()
			if err != nil {
				continue
			}
			ppid, err := p.Ppid()
			if err != nil {
				continue
			}
			result = append(result, &Process{cmdline, ppid})
		}
		return result, nil
	}
)

type Exporter struct {
	mainlog   string
	rejectlog string
	paniclog  string
	eximBin   string
	inputPath string
	logger    log.Logger
}

func NewExporter(mainlog string, rejectlog string, paniclog string, eximExec string, inputPath string, logger log.Logger) *Exporter {
	return &Exporter{
		mainlog,
		rejectlog,
		paniclog,
		eximExec,
		inputPath,
		logger,
	}
}

func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	ch <- eximUp
	ch <- eximQueue
	ch <- eximProcesses
}

func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	states := e.ProcessStates()
	up := float64(0)
	if _, ok := states["daemon"]; ok {
		up = 1
	}
	ch <- prometheus.MustNewConstMetric(eximUp, prometheus.GaugeValue, up)
	for label, value := range states {
		ch <- prometheus.MustNewConstMetric(eximProcesses, prometheus.GaugeValue, value, label)
	}
	queue := e.QueueSize()
	if queue >= 0 {
		ch <- prometheus.MustNewConstMetric(eximQueue, prometheus.GaugeValue, queue)
	}
}

func (e *Exporter) ProcessStates() map[string]float64 {
	level.Debug(e.logger).Log("msg", "Reading process states")
	var states = map[string]float64{}
	processes, err := getProcesses()
	if err != nil {
		level.Error(e.logger).Log("msg", err)
		return states
	}
	for _, p := range processes {
		if len(p.cmdline) < 1 || path.Base(p.cmdline[0]) != e.eximBin {
			continue
		}
		if len(p.cmdline) < 2 {
			states["other"] += 1
		} else if state, ok := processFlags[p.cmdline[1]]; ok {
			if state == "handling" && p.ppid == 1 {
				states["daemon"] += 1
			} else {
				states[state] += 1
			}
		} else {
			states["other"] += 1
		}
	}
	return states
}

func (e *Exporter) QueueSize() float64 {
	level.Debug(e.logger).Log("msg", "Reading queue size")
	count := 0.0
	for h := 0; h < 62; h++ {
		hashPath := filepath.Join(e.inputPath, string(BASE62[h]))
		hashDir, err := os.Open(hashPath)
		if err != nil {
			continue
		}
		messages, err := hashDir.Readdirnames(-1)
		hashDir.Close()
		if err != nil {
			continue
		}
		for _, name := range messages {
			if len(name) == 18 && strings.HasSuffix(name, "-H") {
				count += 1
			}
		}
	}
	return count
}

func (e *Exporter) Start() {
	go e.TailMainLog()
	go e.TailRejectLog()
	go e.TailPanicLog()
}

func (e *Exporter) Tail(filename string) *tail.Tail {
	level.Info(e.logger).Log("msg", "Opening log", "filename", filename)
	logger := log.NewStdlibAdapter(e.logger)
	t, err := tail.TailFile(filename, tail.Config{
		Location: &tail.SeekInfo{Whence: io.SeekEnd},
		ReOpen:   true,
		Follow:   true,
		Logger:   stdlog.New(logger, "", stdlog.LstdFlags),
	})
	if err != nil {
		level.Error(e.logger).Log("msg", "Unable to open log", "err", err)
		os.Exit(1)
	}
	return t
}

func (e *Exporter) TailMainLog() {
	t := e.Tail(e.mainlog)
	for line := range t.Lines {
		level.Debug(e.logger).Log("file", "mainlong", "msg", line.Text)
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
	t := e.Tail(e.rejectlog)
	for line := range t.Lines {
		level.Debug(e.logger).Log("file", "rejectlog", "msg", line.Text)
		eximReject.Inc()
	}
}

func (e *Exporter) TailPanicLog() {
	t := e.Tail(e.paniclog)
	for line := range t.Lines {
		level.Debug(e.logger).Log("file", "paniclog", "msg", line.Text)
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
		mainlog       = kingpin.Flag("exim.mainlog", "Path to Exim main log file.").Default("/var/log/exim4/mainlog").Envar("EXIM_MAINLOG").String()
		rejectlog     = kingpin.Flag("exim.rejectlog", "Path to Exim reject log file.").Default("/var/log/exim4/rejectlog").Envar("EXIM_REJECTLOG").String()
		paniclog      = kingpin.Flag("exim.paniclog", "Path to Exim panic log file.").Default("/var/log/exim4/paniclog").Envar("EXIM_PANICLOG").String()
		eximExec      = kingpin.Flag("exim.executable", "Name of the Exim daemon executable.").Default("exim4").Envar("EXIM_EXECUTABLE").String()
		inputPath     = kingpin.Flag("exim.input-path", "Path to Exim queue directory.").Default("/var/spool/exim4/input").Envar("EXIM_QUEUE_DIR").String()
		listenAddress = kingpin.Flag("web.listen-address", "Address to listen on for web interface and telemetry.").Default(":9350").Envar("WEB_LISTEN_ADDRESS").String()
		metricsPath   = kingpin.Flag("web.telemetry-path", "Path under which to expose metrics.").Default("/metrics").Envar("WEB_TELEMETRY_PATH").String()
	)
	promlogConfig := &promlog.Config{}
	flag.AddFlags(kingpin.CommandLine, promlogConfig)
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()
	logger := promlog.New(promlogConfig)

	level.Info(logger).Log("msg", "Starting exim exporter", "version", version.Info())
	level.Info(logger).Log("msg", "Build context", "context", version.BuildContext())

	exporter := NewExporter(
		*mainlog,
		*rejectlog,
		*paniclog,
		*eximExec,
		*inputPath,
		logger,
	)
	exporter.QueueSize()
	exporter.Start()
	prometheus.MustRegister(exporter)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_, err := w.Write([]byte(`<html>
<head><title>Exim Exporter</title></head>
<body>
  <h1>Exim Exporter</h1>
  <p>` + version.Info() + `</p>
  <p><a href='` + *metricsPath + `'>Metrics</a></p>
</body>
</html>`))
		if err != nil {
			_ = level.Error(logger).Log("msg", err)
		}
	})
	http.Handle(*metricsPath, promhttp.Handler())
	level.Info(logger).Log("msg", "Listening", "address", listenAddress)
	level.Error(logger).Log(http.ListenAndServe(*listenAddress, nil))

}
