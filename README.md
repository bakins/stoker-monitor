# stoker-monitor

Simple monitor for [Stoker BBQ Controller](https://www.rocksbarbque.com/) to 
expose [Prometheus](https://prometheus.io/) metrics.

## Status

This is fairly rough, but works well enough for simple home usage.

## Functionality

`stoker-monitor` uses the [JSON status endpoint](http://kaytat.com/blog/?p=98) 
of the Stoker.

`stoker-monitor` exposes probe status as Prometheus metrics: 

```shell
$ curl http://localhost:7070/metrics
# HELP stoker_blower_state blower state
# TYPE stoker_blower_state gauge
stoker_blower_state{id="3C0000001ADE7605",name="big_green_egg"} 0
# HELP stoker_collections_total number of times data has been collected
# TYPE stoker_collections_total counter
stoker_collections_total 13
# HELP stoker_failures_total number of errors while collecting metrics
# TYPE stoker_failures_total counter
stoker_failures_total 3
# HELP stoker_sensor_temperature sensor temperature
# TYPE stoker_sensor_temperature gauge
stoker_sensor_temperature{blower="",id="0E0000110A4E5730",name="4_food_sensor"} 138.9
stoker_sensor_temperature{blower="",id="2B0000110A442730",name="6_food_sensor"} 149.5
stoker_sensor_temperature{blower="3C0000001ADE7605",id="2A0000110A314B30",name="pit_sensor"} 234.8
```

The values for `stoker_probe_status` is the probes reported temperature in Fahrenheit.

A simple [Prometheus configuration](./prometheus.yml) is included for scraping
`stoker-monitor`.  I run Prometheus and `stoker-monitor` locally while smoking
my meats.

## Building

Requires a working Go development environment.

Clone this repository, cd into the copy, and then run `go build`.

## TODO

*

## Compatibility

I have only tested with the older black wifi units.

## LICENSE

[MIT LICENSE](./LICENSE)