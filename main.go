package main

import (
	"context"
	"encoding/json"
	"flag"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

type sensor struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	Temp   float64 `json:"tc"`
	Blower *string `json:"blower"`
}

type blower struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	On   int    `json:"on"`
}

type stokerResponse struct {
	Stoker struct {
		Sensors []sensor `json:"sensors"`
		Blowers []blower `json:"blowers"`
	} `json:"stoker"`
}

type collector struct {
	sync.Mutex
	interval          time.Duration
	client            *http.Client
	stokerURL         *url.URL
	sensors           map[string]sensor
	sensorMetrics     *prometheus.Desc
	blowers           map[string]blower
	blowerMetrics     *prometheus.Desc
	collections       int64
	collectionMetrics *prometheus.Desc
	failures          int64
	failureMetrics    *prometheus.Desc
	logger            *zap.Logger
}

const metricsNamespace = "stoker"

func newFuncMetric(metricName string, docString string, labels []string) *prometheus.Desc {
	return prometheus.NewDesc(
		prometheus.BuildFQName(metricsNamespace, "", metricName),
		docString, labels, nil,
	)
}

func newCollector(stokerURL string) (*collector, error) {
	u, err := url.Parse(stokerURL)
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to parse url %q", stokerURL)
	}

	c := &collector{
		client:            &http.Client{},
		interval:          time.Second * 10,
		stokerURL:         u,
		sensors:           make(map[string]sensor),
		sensorMetrics:     newFuncMetric("sensor_temperature", "sensor temperature", []string{"id", "name", "blower"}),
		blowers:           make(map[string]blower),
		blowerMetrics:     newFuncMetric("blower_state", "blower state", []string{"id", "name"}),
		collectionMetrics: newFuncMetric("collections_total", "number of times data has been collected", nil),
		failureMetrics:    newFuncMetric("failures_total", "number of errors while collecting metrics", nil),
	}

	l, err := zap.NewProduction()
	if err != nil {
		return nil, errors.Wrap(err, "failed to create logger")
	}

	c.logger = l
	return c, nil
}

func (c *collector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.sensorMetrics
	ch <- c.blowerMetrics
	ch <- c.collectionMetrics
	ch <- c.failureMetrics
}

// TODO: record durations for communicating with stoker?
func (c *collector) getStokerStatus() (*stokerResponse, error) {
	req := &http.Request{
		Method:     http.MethodGet,
		URL:        c.stokerURL,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     make(http.Header),
		Host:       c.stokerURL.Host,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req = req.WithContext(ctx)

	res, err := c.client.Do(req)

	if err != nil {
		return nil, errors.Wrap(err, "http request failed")
	}

	defer res.Body.Close()

	if res.StatusCode != 200 {
		return nil, errors.Wrapf(err, "unexpected HTTP status %d", res.StatusCode)
	}

	data, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read response body")
	}

	var s stokerResponse

	if err := json.Unmarshal(data, &s); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal response body")
	}

	// ensure we have valid data
	if len(s.Stoker.Sensors) == 0 {
		return nil, errors.New("no sensors found")
	}

	return &s, nil

}

var nameLabelRegex = regexp.MustCompile("[^a-z0-9_]+")

func cleanName(in string) string {
	in = strings.Replace(in, " ", "_", -1)
	in = strings.ToLower(in)
	return nameLabelRegex.ReplaceAllString(in, "")
}

func (c *collector) recordMetrics() error {
	s, err := c.getStokerStatus()
	if err != nil {
		return errors.Wrap(err, "failed to get stoker status")
	}

	sensors := make(map[string]sensor, len(s.Stoker.Sensors))

	for _, v := range s.Stoker.Sensors {
		if v.ID == "" {
			// this should never happen
			continue
		}
		v.Name = cleanName(v.Name)
		// make a copy
		sensors[v.ID] = v
	}

	blowers := make(map[string]blower, len(s.Stoker.Blowers))
	for _, v := range s.Stoker.Blowers {
		if v.ID == "" {
			// this should never happen
			continue
		}
		v.Name = cleanName(v.Name)
		// make a copy
		blowers[v.ID] = v
	}

	c.Lock()
	defer c.Unlock()

	// just set to the new values. we do not need to merge
	c.sensors = sensors
	c.blowers = blowers

	return nil
}

func doMetrics(c *collector) {
	errCount := int64(0)
	err := c.recordMetrics()
	if err != nil {
		errCount = 1
		c.logger.Error(
			"failed to record metrics",
			zap.Error(err),
		)
	}
	c.Lock()
	defer c.Unlock()
	c.collections++
	c.failures += errCount
}

func (c *collector) loop(ctx context.Context) {
	doMetrics(c)

	t := time.NewTicker(c.interval)

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			doMetrics(c)
		}
	}
}

// I don't want to lock the collector the entire time I'm waiting
// oon the channel. this is probably not needed
func (c *collector) createMetrics() []prometheus.Metric {
	c.Lock()
	defer c.Unlock()

	var metrics []prometheus.Metric

	m, err := prometheus.NewConstMetric(
		c.failureMetrics,
		prometheus.CounterValue,
		float64(c.failures),
	)
	if err == nil {
		metrics = append(metrics, m)
	} else {
		c.logger.Error(
			"failed to create metric",
			zap.Error(err),
			zap.String("metric", "failureMetrics"),
		)
	}

	m, err = prometheus.NewConstMetric(
		c.collectionMetrics,
		prometheus.CounterValue,
		float64(c.collections),
	)

	if err == nil {
		metrics = append(metrics, m)
	} else {
		c.logger.Error(
			"failed to create metric",
			zap.Error(err),
			zap.String("metric", "collectionMetrics"),
		)
	}

	for _, v := range c.sensors {
		blower := ""
		if v.Blower != nil {
			blower = *v.Blower
		}
		m, err := prometheus.NewConstMetric(
			c.sensorMetrics,
			prometheus.GaugeValue,
			v.Temp,
			v.ID, v.Name, blower,
		)
		if err == nil {
			metrics = append(metrics, m)
		} else {
			c.logger.Error(
				"failed to create metric",
				zap.Error(err),
				zap.String("metric", "sensorMetrics"),
			)
		}
	}

	for _, v := range c.blowers {
		m, err := prometheus.NewConstMetric(
			c.blowerMetrics,
			prometheus.GaugeValue,
			float64(v.On),
			v.ID, v.Name,
		)
		if err == nil {
			metrics = append(metrics, m)
		} else {
			c.logger.Error(
				"failed to create metric",
				zap.Error(err),
				zap.String("metric", "blowerMetrics"),
			)
		}
	}

	return metrics
}
func (c *collector) Collect(ch chan<- prometheus.Metric) {
	metrics := c.createMetrics()

	for _, m := range metrics {
		m := m
		ch <- m
	}
}

/*
Normal exporters would contact the backend when prometheus
scrapes it. However, I get frequent timeouts and other errors
from my stoker unit, so we collect it in the back ground
*/

func main() {
	stoker := flag.String("stoker", "http://192.168.1.103:80/stoker.json", "url for stoker json interface")
	addr := flag.String("address", "127.0.0.1:7070", "listening address for HTTP server")
	flag.Parse()

	c, err := newCollector(*stoker)
	if err != nil {
		panic(err)
	}

	if err := prometheus.Register(c); err != nil {
		panic(errors.Wrap(err, "failed to register metrics"))
	}

	prometheus.Unregister(prometheus.NewProcessCollector(os.Getpid(), ""))
	prometheus.Unregister(prometheus.NewGoCollector())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// TODO: signal handling
	go func() {
		c.loop(ctx)
	}()

	http.Handle("/metrics", promhttp.Handler())

	if err := http.ListenAndServe(*addr, nil); err != nil {
		panic(err)
	}
}
