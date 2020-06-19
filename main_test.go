package main

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/prometheus/common/promlog"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func CreateFiles(names ...string) {
	for _, name := range names {
		fh, err := os.Create(name)
		if err != nil {
			log.Fatal(err)
		}
		fh.Close()
	}
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
			execCommand = func(name string, arg ...string) *exec.Cmd {
				return exec.Command("test/" + name + "." + tc + ".sh")
			}
			metrics, err := os.Open("test/" + tc + ".metrics")
			if err != nil {
				t.Fatalf("Error opening test metrics")
			}
			if err := testutil.CollectAndCompare(exporter, metrics); err != nil {
				t.Fatal("Unexpected metrics returned:", err)
			}
		})
	}
}
