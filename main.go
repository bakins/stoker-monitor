// https://www.rocksbarbque.com - the stoker
// when you connect to telnet port, it is streaming updates about
// once per second.  I was able to figure out which field was temperature
// and how to tell food and pit probes apart.
// Plan is to dump data to statsd(?) and/or into something like Circonus.

package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

/*
sample output from stoker

2B0000110A442730: 1.0 4.0 39.2 -7.5 -0.2 0.2 -0.0 0.3 32.4
0E0000110A4E5730: 1.0 3.6 38.5 -7.5 -0.2 0.1 -0.0 -0.1 31.8
2A0000110A314B30: 1.0 3.8 38.8 142.5 3.6 0.1 3.7 91.7 197.0 PID: NORM tgt:107.2 error:77.7 drive:2.0 istate:18.2 on:1 off:0 blwr:on

*/
func reader(i io.Reader) {
	r := bufio.NewReaderSize(i, 1024)
	for {
		line, _, err := r.ReadLine()
		if err != nil {
			return
		}
		//fmt.Println(string(line))
		parts := strings.Split(string(line), " ")

		l := len(parts)

		switch {
		case l == 11:
			//foodprobe
			fmt.Println("food: ", parts[9])
		case l > 11:
			//pit
			if parts[10] == "PID:" {
				fmt.Println("pit: ", parts[9])
			}
		}
	}
}

func main() {
	c, err := net.DialTimeout("tcp", "192.168.1.103:23", time.Second*10)
	if err != nil {
		panic(err)
	}
	defer c.Close()

	reader(c)
}
