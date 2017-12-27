package main

import (
	"bufio"
	"flag"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

/*
sample output from stoker

ID:               ?   ?   ?    ?    ?    ?    ?   C   F
2B0000110A442730: 1.0 4.0 39.2 -7.5 -0.2 0.2 -0.0 0.3 32.4
0E0000110A4E5730: 1.0 3.6 38.5 -7.5 -0.2 0.1 -0.0 -0.1 31.8
2A0000110A314B30: 1.0 3.8 38.8 142.5 3.6 0.1 3.7 91.7 197.0 PID: NORM tgt:107.2 error:77.7 drive:2.0 istate:18.2 on:1 off:0 blwr:on

*/

type probe struct {
	id          string
	probeType   string  // food or pit
	temperature float64 // temp in C
	blower      string  // blower state - only valid if probeType is pit
	collections int64   // how many times we have collected it
}

type collector struct {
	sync.Mutex
	probes              map[string]*probe
	probeMetrics        *prometheus.Desc
	collectionMetrics   *prometheus.Desc
	collectErrors       map[string]int64
	collectErrorMetrics *prometheus.Desc
	logger              *zap.Logger
}

const metricsNamespace = "stoker"

func newFuncMetric(metricName string, docString string, labels []string) *prometheus.Desc {
	return prometheus.NewDesc(
		prometheus.BuildFQName(metricsNamespace, "", metricName),
		docString, labels, nil,
	)
}

func newCollector() (*collector, error) {
	c := &collector{
		probes:              make(map[string]*probe),
		probeMetrics:        newFuncMetric("probe_status", "probe status", []string{"type", "id", "blower"}),
		collectionMetrics:   newFuncMetric("probe_collections_total", "number of times data has been collected", []string{"type", "id"}),
		collectErrors:       make(map[string]int64),
		collectErrorMetrics: newFuncMetric("collect_failures_total", "number of errors while collecting metrics", []string{"type"}),
	}

	l, err := zap.NewProduction()
	if err != nil {
		return nil, errors.Wrap(err, "failed to create logger")
	}

	c.logger = l
	return c, nil
}

func (c *collector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.probeMetrics
	ch <- c.collectionMetrics
	ch <- c.collectErrorMetrics
}

func (c *collector) addError(t string) {
	c.Lock()
	defer c.Unlock()

	v, ok := c.collectErrors[t]
	if !ok {
		v = 0
	}
	v++
	c.collectErrors[t] = v
}

func (c *collector) setTemp(parts []string) error {
	var probeType string

	id := parts[0]

	if !strings.HasSuffix(id, ":") {
		// not a probe
		return nil
	}

	id = strings.TrimSuffix(id, ":")

	l := len(parts)
	switch {
	case l == 11:
		probeType = "food"
	case l > 11:
		if parts[10] != "PID:" {
			c.addError("unknownProbe")
			return errors.Errorf("unknown probe type %q", parts[10])
		}
		probeType = "pit"

	default:
		// usually status messages, etc
		return nil
	}

	// temp in C
	t, err := strconv.ParseFloat(parts[8], 32)
	if err != nil {
		c.addError("parseFloat")
		return errors.Wrapf(err, "failed to parse temperature %q", parts[8])
	}

	// TODO: if pit, parse the extra data and set blower

	c.Lock()
	defer c.Unlock()

	p, ok := c.probes[id]
	if ok {
		p.collections++
		p.temperature = t
		p.blower = "unknown"
	} else {

		p = &probe{
			id:          id,
			probeType:   probeType,
			temperature: t,
			blower:      "unknown",
			collections: 1,
		}
		c.probes[p.id] = p
	}

	return nil
}

func (c *collector) reader(i io.Reader) error {
	r := bufio.NewReaderSize(i, 1024)
	for {
		line, _, err := r.ReadLine()
		if err != nil {
			c.addError("readLine")
			return errors.Wrap(err, "ReadLine failed")
		}

		parts := strings.Split(string(line), " ")

		if err := c.setTemp(parts); err != nil {
			c.logger.Error("failed to set probe status", zap.Error(err))
		}
	}
}

func (c *collector) Collect(ch chan<- prometheus.Metric) {
	c.Lock()
	defer c.Unlock()

	for k, v := range c.collectErrors {
		m, err := prometheus.NewConstMetric(
			c.collectErrorMetrics,
			prometheus.CounterValue,
			float64(v),
			k,
		)
		if err != nil {
			c.logger.Error(
				"failed to create metric",
				zap.Error(err),
				zap.String("metric", "errors"),
			)
			continue
		}
		ch <- m
	}

	for _, v := range c.probes {
		m, err := prometheus.NewConstMetric(
			c.probeMetrics,
			prometheus.GaugeValue,
			v.temperature,
			v.probeType, v.id, v.blower,
		)
		if err != nil {
			c.logger.Error(
				"failed to create metric",
				zap.Error(err),
				zap.String("metric", "probes"),
			)

		} else {
			ch <- m
		}

		m, err = prometheus.NewConstMetric(
			c.collectionMetrics,
			prometheus.CounterValue,
			float64(v.collections),
			v.probeType, v.id,
		)
		if err != nil {
			c.logger.Error(
				"failed to create metric",
				zap.Error(err),
				zap.String("metric", "collections"),
			)
			continue
		} else {
			ch <- m
		}
	}

}

func main() {
	stoker := flag.String("stoker", "192.168.1.103:23", "address for stoker telnet interface")
	addr := flag.String("address", "127.0.0.1:8080", "listening address for HTTP server")
	flag.Parse()

	c, err := newCollector()
	if err != nil {
		panic(err)
	}

	if err := prometheus.Register(c); err != nil {
		panic(errors.Wrap(err, "failed to register metrics"))
	}

	prometheus.Unregister(prometheus.NewProcessCollector(os.Getpid(), ""))
	prometheus.Unregister(prometheus.NewGoCollector())

	go func() {
		for {
			s, err := net.DialTimeout("tcp", *stoker, time.Second*10)
			if err != nil {
				c.addError("dial")
				time.Sleep(time.Second)
				continue
			}
			defer s.Close()
			c.reader(s)
		}
	}()

	http.Handle("/metrics", promhttp.Handler())

	if err := http.ListenAndServe(*addr, nil); err != nil {
		panic(err)
	}
}
