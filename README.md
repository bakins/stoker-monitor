# stoker-monitor

Simple monitor for [Stoker BBQ Controller](https://www.rocksbarbque.com/) to 
expose [Prometheus](https://prometheus.io/) metrics.

## Status

This is fairly rough, but works well enough for simple home usage.

## Functionality

`stoker-monitor` uses the telnet interface of the Stoker.  When you connect to
port 23 of a Stoker, it will stream probe information, including the probe
ID and the temperature in C and F.

`stoker-monitor` exposes probe status as Prometheus metrics: 

```shell
$ curl http://localhost:8080/metrics
# HELP stoker_probe_collections_total number of times data has been collected
# TYPE stoker_probe_collections_total counter
stoker_probe_collections_total{id="0E0000110A4E5730",type="food"} 226
stoker_probe_collections_total{id="2A0000110A314B30",type="pit"} 225
stoker_probe_collections_total{id="2B0000110A442730",type="food"} 227
# HELP stoker_probe_status probe status
# TYPE stoker_probe_status gauge
stoker_probe_status{blower="unknown",id="0E0000110A4E5730",type="food"} 31.700000762939453
stoker_probe_status{blower="unknown",id="2A0000110A314B30",type="pit"} 102.0999984741211
stoker_probe_status{blower="unknown",id="2B0000110A442730",type="food"} 41.400001525878906
```

A simple [Prometheus configuration](./prometheus.yml) is included for scraping
`stoker-monitor`.  I run Prometheus and `stoker-monitor` locally while smoking
my meats.

## Building

Requires a working Go development environment.

Clone this repository, cd into the copy, and then run `go build`.

## TODO

* parse and expose blower status
* map probe id to a "friendly" name

## Compatibility

I have only tested with the older black wifi units.

## LICENSE

[MIT LICENSE](./LICENSE)