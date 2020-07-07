package main

import (
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/prometheus/common/promlog"
	"io/ioutil"
	"math/rand"
	"os"
	"path"
	"path/filepath"
	"testing"
	"time"
)

func buildMockInput(inputPath string) error {
	for h := 0; h < 62; h++ {
		hashChar := string(BASE62[h])
		hashPath := path.Join(inputPath, hashChar)
		if err := os.MkdirAll(hashPath, 0755); err != nil {
			return err
		}
		for i := 0; i <= h%3; i++ {
			msgName := ""
			for i := 0; i < 5; i++ {
				msgName += string(BASE62[rand.Intn(62)])
			}
			msgName += hashChar + "-"
			for i := 0; i < 6; i++ {
				msgName += string(BASE62[rand.Intn(62)])
			}
			msgName += "-"
			for i := 0; i < 2; i++ {
				msgName += string(BASE62[rand.Intn(62)])
			}
			for _, fileType := range "HD" {
				fileName := msgName + "-" + string(fileType)
				fh, err := os.Create(path.Join(hashPath, fileName))
				if err != nil {
					return err
				}
				fh.Close()
			}
		}
	}
	return nil
}

func collectAndCompareTestCase(name string, gatherer prometheus.Gatherer, t *testing.T) {
	metrics, err := os.Open(path.Join("test", name+".metrics"))
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

func TestMetrics(t *testing.T) {
	logger := promlog.New(&promlog.Config{})

	// Create a temp dir for our mock data
	tempPath, err := ioutil.TempDir("", "exim_exporter_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempPath)
	inputPath := path.Join(tempPath, "input")
	if err := os.MkdirAll(inputPath, 0755); err != nil {
		t.Fatal(err)
	}

	// Setup temporary log files so we can stream data into them
	mainlog, err := os.OpenFile(filepath.Join(tempPath, "mainlog"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	defer mainlog.Close()
	rejectlog, err := os.OpenFile(filepath.Join(tempPath, "rejectlog"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	defer mainlog.Close()
	paniclog, err := os.OpenFile(filepath.Join(tempPath, "paniclog"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	defer mainlog.Close()

	registry := prometheus.NewPedanticRegistry()
	exporter := NewExporter(
		mainlog.Name(),
		rejectlog.Name(),
		paniclog.Name(),
		"exim4",
		inputPath,
		logger,
	)
	exporter.Start()
	if err := registry.Register(exporter); err != nil {
		t.Fatal(err)
	}

	for _, metric := range []prometheus.Collector{eximMessages, eximReject, eximPanic} {
		if err := registry.Register(metric); err != nil {
			t.Fatal(err)
		}
	}

	getProcesses = func() ([]*Process, error) {
		return []*Process{
			{[]string{"/bin/bash", "-x"}, 1},
		}, nil
	}
	t.Run("down", func(t *testing.T) {
		collectAndCompareTestCase("down", registry, t)
	})

	if err = buildMockInput(inputPath); err != nil {
		t.Fatal("Unable to create mock input:", err)
	}
	getProcesses = func() ([]*Process, error) {
		return []*Process{
			{[]string{"/bin/bash", "-x"}, 7},
			{[]string{"/usr/sbin/exim4"}, 2202},
			{[]string{"/usr/sbin/exim4", "-q30m"}, 2203},
			{[]string{"/usr/sbin/exim4", "-bd"}, 1},
			{[]string{"/usr/sbin/exim4", "-qG"}, 2211},
			{[]string{"/usr/sbin/exim4", "-Mc", "1jofsL-0006tb-8D"}, 2309},
			{[]string{"/usr/sbin/exim4", "-Mc", "1jofsL-0006tb-8D"}, 2315},
			{[]string{"/usr/sbin/exim4", "-bd"}, 3147},
			{[]string{"/usr/sbin/exim4", "-bd"}, 3148},
			{[]string{"/usr/sbin/exim4", "-bd"}, 3149},
		}, nil
	}
	t.Run("up", func(t *testing.T) {
		collectAndCompareTestCase("up", registry, t)
	})
	t.Run("tail", func(t *testing.T) {
		fmt.Println("---")
		appendLog("mainlog", mainlog, t)
		appendLog("rejectlog", rejectlog, t)
		appendLog("paniclog", paniclog, t)
		time.Sleep(100 * time.Millisecond)
		collectAndCompareTestCase("tail", registry, t)
	})
	t.Run("update", func(t *testing.T) {
		fmt.Println("---")
		appendLog("mainlog", mainlog, t)
		appendLog("rejectlog", rejectlog, t)
		appendLog("paniclog", paniclog, t)
		time.Sleep(100 * time.Millisecond)
		collectAndCompareTestCase("update", registry, t)
	})
}
