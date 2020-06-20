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

func CreateFiles(names ...string) {
	for _, name := range names {
		fh, err := os.Create(name)
		if err != nil {
			log.Fatal(err)
		}
		if err := fh.Close(); err != nil {
			log.Fatal(err)
		}
	}
}

func mockCommand(test string, env ...string) func(command string, args ...string) *exec.Cmd {
	return func(command string, args ...string) *exec.Cmd {
		cs := []string{"-test.run=TestHelperProcess", "--", command}
		cs = append(cs, args...)
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = append(env, "GO_WANT_HELPER_PROCESS=1", "MOCK_COMMAND_TEST_CASE="+test)
		return cmd
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
	defer func() { execCommand = exec.Command }()
	logger := promlog.New(&promlog.Config{})
	dir, err := ioutil.TempDir("", "exim_exporter_test")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)

	mainlog := filepath.Join(dir, "mainlog")
	rejectlog := filepath.Join(dir, "mainlog")
	paniclog := filepath.Join(dir, "mainlog")
	CreateFiles(mainlog, rejectlog, paniclog)
	exporter := &Exporter{
		&mainlog,
		&rejectlog,
		&paniclog,
		logger,
	}
	exporter.Start()
	prometheus.MustRegister(exporter)

	testCases := []string{"down", "up"}
	for _, tc := range testCases {
		t.Run(tc, func(t *testing.T) {
			execCommand = mockCommand(tc)
			metrics, err := os.Open(path.Join("test", tc, "metrics"))
			if err != nil {
				t.Fatalf("Error opening test metrics")
			}
			if err := testutil.CollectAndCompare(exporter, metrics); err != nil {
				t.Fatal("Unexpected metrics returned:", err)
			}
		})
	}
}
