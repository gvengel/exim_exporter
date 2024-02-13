package main

import (
	"bufio"
	"io"
	stdlog "log"
	"log/syslog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

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
	inputPath        = kingpin.Flag("exim.input-path", "Path to Exim queue directory.").Default("/var/spool/exim4/input").Envar("EXIM_QUEUE_DIR").Envar("EXIM_INPUT_PATH").String()
	frozenTimeout    = kingpin.Flag("exim.frozen-timeout", "Number of seconds before reading frozen messages is aborted").Default("5s").Envar("EXIM_FROZEN_TIMEOUT").Duration()
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
	eximQueueFrozen = prometheus.NewDesc(
		prometheus.BuildFQName("exim", "", "queue_frozen"),
		"Number of messages currently frozen in queue",
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
		[]string{"flag"},
	)
	eximMessageErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: prometheus.BuildFQName("exim", "", "message_errors_total"),
			Help: "Number of logged messages broken down by error code (451, 550, etc)",
		},
		[]string{"status", "enhanced"},
	)
	eximReject = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: prometheus.BuildFQName("exim", "", "reject_total"),
			Help: "Total number of logged reject messages",
		},
	)
	eximPanic = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: prometheus.BuildFQName("exim", "", "panic_total"),
			Help: "Total number of logged panic messages",
		},
	)
	readErrors = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: prometheus.BuildFQName("exim", "", "log_read_errors"),
			Help: "Total number of errors encountered while reading the logs",
		},
	)
	timeoutErrors = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: prometheus.BuildFQName("exim", "", "queue_state_timeout_errors"),
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

// Basic status code (https://datatracker.ietf.org/doc/html/rfc821)
// followed by optional Enhanced status code (https://datatracker.ietf.org/doc/html/rfc3463)
var errorCodeRegexp = regexp.MustCompile(": ([2-5][0-9]{2})[ -]([2-5]\\.[0-9]{1,3}\\.[0-9]{1,3})?")

type Exporter struct {
	mainlog   string
	rejectlog string
	paniclog  string
	eximBin   string
	inputPath string
	logLevel  string
	logger    log.Logger
}

type QueueSize struct {
	total    float64
	frozen   float64
	timedOut bool
}

var queueSizeLastTimeout float64

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
	ch <- eximQueueFrozen
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
	ch <- prometheus.MustNewConstMetric(eximQueue, prometheus.GaugeValue, queue.total)
	ch <- prometheus.MustNewConstMetric(eximQueueFrozen, prometheus.GaugeValue, queue.frozen)
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

func (e *Exporter) CountMessages(dirname string, queueSize *QueueSize, deadline time.Time) {
	dir, err := os.Open(dirname)
	if err != nil {
		return
	}
	messages, err := dir.Readdirnames(-1)
	_ = dir.Close()
	if err != nil {
		return
	}
	var lineNumber int
	for _, fileName := range messages {
		// message ID in exim >= 4.97 are 25 chars
		// message ID in exim < 4.97 are only 18 chars
		// Each message has a header and data file, so only count one of them
		if !(len(fileName) == 25 || len(fileName) == 18) || !strings.HasSuffix(fileName, "-H") {
			continue
		}
		queueSize.total += 1

		if !deadline.IsZero() {
			if queueSizeLastTimeout > 0 || queueSize.timedOut {
				continue
			} else if time.Now().After(deadline) {
				queueSize.timedOut = true
				queueSize.frozen = 0
				timeoutErrors.Inc()
				continue
			}
		}

		headerFile, err := os.Open(path.Join(dirname, fileName))
		if err != nil {
			continue
		}
		// https://www.exim.org/exim-html-current/doc/html/spec_html/ch-format_of_spool_files.html
		fileScanner := bufio.NewScanner(headerFile)
		fileScanner.Split(bufio.ScanLines)
		lineNumber = 0
		for fileScanner.Scan() {
			lineNumber++
			// First four lines of the file contain fixed metadata
			if lineNumber <= 4 {
				continue
			}
			// Then follow a number of lines starting with a hyphen.
			// These contain variables, which can appear in any order.
			// If the line doesn't start with a hyphen, then we've reached the
			// end of the variable section.
			if !strings.HasPrefix(fileScanner.Text(), "-") {
				break
			}
			// If we found the frozen flag, stop scanning, since that's all we care about for now.
			if strings.HasPrefix(fileScanner.Text(), "-frozen ") {
				queueSize.frozen++
				break
			}
		}
	}
}

func (e *Exporter) QueueSize() QueueSize {
	_ = level.Debug(e.logger).Log("msg", "Reading queue size")
	timeout := *frozenTimeout
	var deadline time.Time
	if timeout > 0 {
		deadline = time.Now().Add(*frozenTimeout)
	}
	queueSize := QueueSize{}
	e.CountMessages(e.inputPath, &queueSize, deadline)
	for h := 0; h < len(BASE62); h++ {
		hashPath := filepath.Join(e.inputPath, string(BASE62[h]))
		e.CountMessages(hashPath, &queueSize, deadline)
	}
	if queueSize.timedOut {
		queueSizeLastTimeout = queueSize.total
	} else if queueSizeLastTimeout > 0 && queueSize.total < queueSizeLastTimeout*.9 {
		queueSizeLastTimeout = 0
	}
	return queueSize
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
		Location: &tail.SeekInfo{Whence: io.SeekEnd},
		ReOpen:   true,
		Follow:   true,
		Poll:     *tailPoll,
		Logger:   logger,
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
			_ = level.Error(e.logger).Log("msg", "Caught errorFlag while reading mainlog", "err", line.Err)
			readErrors.Inc()
			continue
		}
		_ = level.Debug(e.logger).Log("file", "mainlong", "msg", line.Text)
		parts := strings.SplitN(line.Text, " ", 7)
		size := len(parts)
		if size < 3 {
			continue
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

		errorFlag := false
		switch parts[index] {
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
			errorFlag = true
		case "==":
			eximMessages.With(prometheus.Labels{"flag": "deferred"}).Inc()
			errorFlag = true
		case "Completed":
			eximMessages.With(prometheus.Labels{"flag": "completed"}).Inc()
		}
		if errorFlag {
			match := errorCodeRegexp.FindStringSubmatch(line.Text)
			if len(match) > 0 {
				eximMessageErrors.With(prometheus.Labels{"status": match[1], "enhanced": match[2]}).Inc()
			}
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
		eximReject.Inc()
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
	prometheus.MustRegister(eximMessageErrors)
	prometheus.MustRegister(readErrors)
	prometheus.MustRegister(timeoutErrors)
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
