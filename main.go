package main

import (
	"io"
	stdlog "log"
	"log/syslog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/alecthomas/kingpin/v2"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"

	"github.com/prometheus/client_golang/prometheus"
	version_collector "github.com/prometheus/client_golang/prometheus/collectors/version"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/promlog"
	"github.com/prometheus/common/promlog/flag"
	"github.com/prometheus/common/version"
	"github.com/prometheus/exporter-toolkit/web"

	"github.com/nxadm/tail"
	"github.com/shirou/gopsutil/v3/process"
)

var (
	logPath          = kingpin.Flag("exim.log-path", "Path to Exim panic log file.").Default("/var/log/exim4").Envar("EXIM_LOG_PATH").String()
	mainlog          = kingpin.Flag("exim.mainlog", "Path to Exim main log file.").Default("mainlog").Envar("EXIM_MAINLOG").String()
	rejectlog        = kingpin.Flag("exim.rejectlog", "Path to Exim reject log file.").Default("rejectlog").Envar("EXIM_REJECTLOG").String()
	paniclog         = kingpin.Flag("exim.paniclog", "Path to Exim panic log file.").Default("paniclog").Envar("EXIM_PANICLOG").String()
	eximExec         = kingpin.Flag("exim.executable", "Name of the Exim daemon executable.").Default("exim4").Envar("EXIM_EXECUTABLE").String()
	inputPath        = kingpin.Flag("exim.input-path", "Path to Exim queue directory.").Default("/var/spool/exim4/input").Envar("EXIM_QUEUE_DIR").String()
	listenAddress    = kingpin.Flag("web.listen-address", "Address to listen on for web interface and telemetry.").Default(":9636").Envar("WEB_LISTEN_ADDRESS").String()
	metricsPath      = kingpin.Flag("web.telemetry-path", "Path under which to expose metrics.").Default("/metrics").Envar("WEB_TELEMETRY_PATH").String()
	useJournal       = kingpin.Flag("exim.use-journal", "Use the journal instead of log file tailing").Envar("EXIM_USE_JOURNAL").Bool()
	syslogIdentifier = kingpin.Flag("exim.syslog-identifier", "Syslog identifier used by Exim").Default("exim").Envar("EXIM_SYSLOG_IDENTIFIER").String()
	tailPoll         = kingpin.Flag("tail.poll", "Poll logs for changes instead of using inotify.").Envar("TAIL_POLL").Bool()
	webConfigFile    = kingpin.Flag("web.config.file", "[EXPERIMENTAL] Path to configuration file that can enable TLS or authentication.").Default("").Envar("WEB_CONFIG_FILE").String()
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
		prometheus.BuildFQName("exim", "", "processes"),
		"Number of running exim process broken down by state (delivering, handling, etc)",
		[]string{"state"}, nil,
	)
	eximMessages = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: prometheus.BuildFQName("exim", "", "messages_total"),
			Help: "Total number of logged messages broken down by flag (delivered, deferred, etc)",
		},
		[]string{"flag", "router", "transport"},
	)
	eximReject = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: prometheus.BuildFQName("exim", "", "reject_total"),
			Help: "Total number of logged reject messages",
		},
		[]string{"reason"},
	)
	eximPanic = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: prometheus.BuildFQName("exim", "", "panic_total"),
			Help: "Total number of logged panic messages",
		},
	)
	readErrors = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: prometheus.BuildFQName("exim", "log_read", "errors"),
			Help: "Total number of errors encountered while reading the logs",
		},
	)
)

var processFlags = map[string]string{
	"-Mc":  "delivering",
	"-bd":  "handling",
	"-bdf": "handling",
	"-q":   "running",
	"-qG":  "running",
}

type Process struct {
	cmdline []string
	leader  bool
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
			pid := int(p.Pid)
			pgid, err := syscall.Getpgid(pid)
			if err != nil {
				continue
			}
			leader := pid == pgid
			result = append(result, &Process{cmdline, leader})
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
	logLevel  string
	logger    log.Logger
}

func NewExporter(mainlog, rejectlog, paniclog, eximExec, inputPath, logLevel string, logger log.Logger) *Exporter {
	return &Exporter{
		mainlog,
		rejectlog,
		paniclog,
		eximExec,
		inputPath,
		logLevel,
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
	_ = level.Debug(e.logger).Log("msg", "Reading process states")
	states := make(map[string]float64)
	processes, err := getProcesses()
	if err != nil {
		_ = level.Error(e.logger).Log("msg", err)
		return states
	}
	for _, p := range processes {
		if len(p.cmdline) < 1 || path.Base(p.cmdline[0]) != e.eximBin {
			continue
		}
		if len(p.cmdline) < 2 {
			states["other"] += 1
		} else if state, ok := processFlags[p.cmdline[1]]; ok {
			if state == "handling" && p.leader {
				states["daemon"] += 1
			} else {
				states[state] += 1
			}
		} else {
			isDaemon := false
			if p.leader {
				for _, arg := range p.cmdline {
					if arg == "-bd" || arg == "-bdf" {
						isDaemon = true
					}
				}
			}
			if isDaemon {
				states["daemon"] += 1
			} else {
				states["other"] += 1
			}
		}
	}
	return states
}

func (e *Exporter) CountMessages(dirname string) float64 {
	dir, err := os.Open(dirname)
	if err != nil {
		return 0
	}
	messages, err := dir.Readdirnames(-1)
	_ = dir.Close()
	if err != nil {
		return 0
	}
	var count float64
	for _, name := range messages {
		if (len(name) == 18 || len(name) == 25) && strings.HasSuffix(name, "-H") {
			count += 1
		}
	}
	return count
}

func (e *Exporter) QueueSize() float64 {
	_ = level.Debug(e.logger).Log("msg", "Reading queue size")
	count := e.CountMessages(e.inputPath)
	for h := 0; h < len(BASE62); h++ {
		hashPath := filepath.Join(e.inputPath, string(BASE62[h]))
		count += e.CountMessages(hashPath)
	}
	return count
}

func (e *Exporter) Start() {
	if *useJournal {
		go e.TailMainLog(e.JournalTail(*syslogIdentifier, syslog.LOG_INFO))
		go e.TailRejectLog(e.JournalTail(*syslogIdentifier, syslog.LOG_NOTICE))
		go e.TailPanicLog(e.JournalTail(*syslogIdentifier, syslog.LOG_ALERT))
	} else {
		go e.TailMainLog(e.FileTail(e.mainlog))
		go e.TailRejectLog(e.FileTail(e.rejectlog))
		go e.TailPanicLog(e.FileTail(e.paniclog))
	}
}

func (e *Exporter) FileTail(filename string) chan *tail.Line {
	_ = level.Info(e.logger).Log("msg", "Opening log", "filename", filename)
	var logger *stdlog.Logger
	if e.logLevel == "debug" || e.logLevel == "info" {
		adapter := log.NewStdlibAdapter(e.logger)
		logger = stdlog.New(adapter, "", stdlog.LstdFlags)
	} else {
		logger = tail.DiscardingLogger
	}
	t, err := tail.TailFile(filename, tail.Config{
		Location:      &tail.SeekInfo{Whence: io.SeekEnd},
		ReOpen:        true,
		Follow:        true,
		CompleteLines: true,
		Poll:          *tailPoll,
		Logger:        logger,
	})
	if err != nil {
		_ = level.Error(e.logger).Log("msg", "Unable to open log", "err", err)
		os.Exit(1)
	}
	return t.Lines
}

// JournalTail conditionally defined based on the "systemd" build tag.

func (e *Exporter) TailMainLog(lines chan *tail.Line) {
	for line := range lines {
		if line.Err != nil {
			_ = level.Error(e.logger).Log("msg", "Caught error while reading mainlog", "err", line.Err)
			readErrors.Inc()
			continue
		}
		_ = level.Debug(e.logger).Log("file", "mainlog", "msg", line.Text)
		parts := strings.SplitN(line.Text, " ", 100)
		size := len(parts)
		if size < 3 {
			continue
		}

		router := ""
		transport := ""
		for _, v := range parts {
			if strings.HasPrefix(v, "R=") {
				// this is the Router
				router, _ = strings.CutPrefix(v, "R=")
				_ = level.Debug(e.logger).Log("file", "mainlog", "msg", "found router", router)
			}
			if strings.HasPrefix(v, "T=") {
				// this is the Transport
				transport, _ = strings.CutPrefix(v, "T=")
				_ = level.Debug(e.logger).Log("file", "mainlog", "msg", "found transport", transport)
			}
		}

		index := 2
		// Handle logs when timestamps are enabled
		if parts[index][0] == '+' || parts[index][0] == '-' {
			index++
		}

		// Handle logs when PID logging is enabled
		if parts[index][0] == '[' {
			index++
		}

		// Increment once more to get past the mail ID
		index++

		if size < index+1 {
			continue
		}

		switch parts[index] {
		case "<=":
			eximMessages.With(prometheus.Labels{"flag": "arrived", "router": "", "transport": ""}).Inc()
		case "(=":
			eximMessages.With(prometheus.Labels{"flag": "fakereject", "router": "", "transport": ""}).Inc()
		case "=>":
			eximMessages.With(prometheus.Labels{"flag": "delivered", "router": router, "transport": transport}).Inc()
		case "->":
			eximMessages.With(prometheus.Labels{"flag": "additional", "router": "", "transport": ""}).Inc()
		case ">>":
			eximMessages.With(prometheus.Labels{"flag": "cutthrough", "router": "", "transport": ""}).Inc()
		case "*>":
			eximMessages.With(prometheus.Labels{"flag": "suppressed", "router": "", "transport": ""}).Inc()
		case "**":
			eximMessages.With(prometheus.Labels{"flag": "failed", "router": router, "transport": transport}).Inc()
		case "==":
			eximMessages.With(prometheus.Labels{"flag": "deferred", "router": router, "transport": transport}).Inc()
		case "Completed":
			eximMessages.With(prometheus.Labels{"flag": "completed", "router": "", "transport": ""}).Inc()
		}
	}
}

func (e *Exporter) TailRejectLog(lines chan *tail.Line) {
	for line := range lines {
		if line.Err != nil {
			_ = level.Error(e.logger).Log("msg", "Caught error while reading rejectlog", "err", line.Err)
			readErrors.Inc()
			continue
		}
		_ = level.Debug(e.logger).Log("file", "rejectlog", "msg", line.Text)

		if strings.HasSuffix(line.Text, "SPF check failed.") {
			eximReject.With(prometheus.Labels{"reason": "SPF check failed"}).Inc()
		} else if strings.HasSuffix(line.Text, "relay not permitted") {
			eximReject.With(prometheus.Labels{"reason": "relay not permitted"}).Inc()
		} else if strings.Contains(line.Text, "535 Incorrect authentication data") {
			eximReject.With(prometheus.Labels{"reason": "535 Incorrect authentication data"}).Inc()
		} else {
			eximReject.With(prometheus.Labels{"reason": "other"}).Inc()
		}
	}
}

func (e *Exporter) TailPanicLog(lines chan *tail.Line) {
	for line := range lines {
		if line.Err != nil {
			_ = level.Error(e.logger).Log("msg", "Caught error while reading paniclog", "err", line.Err)
			readErrors.Inc()
			continue
		}
		_ = level.Debug(e.logger).Log("file", "paniclog", "msg", line.Text)
		eximPanic.Inc()
	}
}

func init() {
	prometheus.MustRegister(version_collector.NewCollector("exim_exporter"))
	prometheus.MustRegister(eximMessages)
	prometheus.MustRegister(eximReject)
	prometheus.MustRegister(eximPanic)
	prometheus.MustRegister(readErrors)
}

func main() {
	promlogConfig := &promlog.Config{}
	flag.AddFlags(kingpin.CommandLine, promlogConfig)
	kingpin.Version(version.Print("exim_exporter"))
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()
	logger := promlog.New(promlogConfig)

	_ = level.Info(logger).Log("msg", "Starting exim exporter", "version", version.Info())
	_ = level.Info(logger).Log("msg", "Build context", "context", version.BuildContext())

	if !path.IsAbs(*mainlog) {
		*mainlog = path.Join(*logPath, *mainlog)
	}
	if !path.IsAbs(*rejectlog) {
		*rejectlog = path.Join(*logPath, *rejectlog)
	}
	if !path.IsAbs(*paniclog) {
		*paniclog = path.Join(*logPath, *paniclog)
	}

	exporter := NewExporter(
		*mainlog,
		*rejectlog,
		*paniclog,
		*eximExec,
		*inputPath,
		promlogConfig.Level.String(),
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

	_ = level.Info(logger).Log("msg", "Listening", "address", listenAddress)
	server := &http.Server{}
	webSystemdSocket := false
	flags := web.FlagConfig{
		WebListenAddresses: &[]string{*listenAddress},
		WebSystemdSocket:   &webSystemdSocket,
		WebConfigFile:      webConfigFile,
	}
	if err := web.ListenAndServe(server, &flags, logger); err != nil {
		_ = level.Error(logger).Log("err", err)
		os.Exit(1)
	}
}
