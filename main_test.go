package main

import (
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/prometheus/common/promlog"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"testing"
)

func mockCommand(test string, env ...string) func(command string, args ...string) *exec.Cmd {
	return func(command string, args ...string) *exec.Cmd {
		cs := []string{"-test.run=TestHelperProcess", "--", command}
		cs = append(cs, args...)
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = append(env, "GO_WANT_HELPER_PROCESS=1", "MOCK_COMMAND_TEST_CASE="+test)
		return cmd
	}
}

func collectAndCompareTestCase(name string, gatherer prometheus.Gatherer, t *testing.T) {
	execCommand = mockCommand(name)
	metrics, err := os.Open(path.Join("test", name, "metrics"))
	if err != nil {
		t.Fatalf("Error opening test metrics")
	}
	if err := testutil.GatherAndCompare(gatherer, metrics); err != nil {
		t.Fatal("Unexpected metrics returned:", err)
	}
}

func appendLog(name string, file *os.File, t *testing.T) {
	data, err := ioutil.ReadFile(path.Join("test", name))
	if err != nil {
		t.Fatal("Unable to read mainlog test data")
	}
	if _, err := file.Write(data); err != nil {
		t.Fatal("Unable to read mainlog test data")
	}
	if err := file.Sync(); err != nil {
		t.Fatal(err)
	}
}

func TestHelperProcess(t *testing.T) {
	if _, ok := os.LookupEnv("GO_WANT_HELPER_PROCESS"); !ok {
		return
	}
	tc, ok := os.LookupEnv("MOCK_COMMAND_TEST_CASE")
	if !ok {
		return
	}
	prog := ""
	for i, arg := range os.Args {
		if arg == "--" {
			prog = os.Args[i+1]
			break
		}
	}
	if prog == "" {
		log.Fatal("Unable to parse program name from command line.")
	}
	outBytes, err := ioutil.ReadFile(path.Join("test", tc, prog+".output"))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Print(string(outBytes))
	os.Exit(0)
}

func TestMetrics(t *testing.T) {
	logger := promlog.New(&promlog.Config{})

	// Setup temporary log files so we can stream data into them
	dir, err := ioutil.TempDir("", "exim_exporter_test")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)
	mainlog, err := os.OpenFile(filepath.Join(dir, "mainlog"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal("Unable to open mainlog")
	}
	defer mainlog.Close()
	rejectlog, err := os.OpenFile(filepath.Join(dir, "rejectlog"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal("Unable to open mainlog")
	}
	defer mainlog.Close()
	paniclog, err := os.OpenFile(filepath.Join(dir, "paniclog"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal("Unable to open mainlog")
	}
	defer mainlog.Close()

	registry := prometheus.NewPedanticRegistry()
	exporter := NewExporter(
		mainlog.Name(),
		rejectlog.Name(),
		paniclog.Name(),
		logger,
	)
	exporter.Start()
	registry.Register(exporter)

	for _, metric := range []prometheus.Collector{eximMessages, eximReject, eximPanic} {
		if err := registry.Register(metric); err != nil {
			t.Fatal(err)
		}
	}

	defer func() { execCommand = exec.Command }()

	t.Run("down", func(t *testing.T) {
		collectAndCompareTestCase("down", registry, t)
	})
	t.Run("up", func(t *testing.T) {
		collectAndCompareTestCase("up", registry, t)
	})
	t.Run("tail", func(t *testing.T) {
		fmt.Println("---")
		appendLog("mainlog", mainlog, t)
		appendLog("rejectlog", rejectlog, t)
		appendLog("paniclog", paniclog, t)
		collectAndCompareTestCase("tail", registry, t)
	})
	t.Run("update", func(t *testing.T) {
		fmt.Println("---")
		appendLog("mainlog", mainlog, t)
		appendLog("rejectlog", rejectlog, t)
		appendLog("paniclog", paniclog, t)
		collectAndCompareTestCase("update", registry, t)
	})
}
