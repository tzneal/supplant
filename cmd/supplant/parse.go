package main

import (
	"log"
	"strconv"
	"strings"
)

type port struct {
	listenPort  int32
	servicePort int32
}

func parseInt(full, num, name string) int32 {
	i, err := strconv.ParseInt(num, 10, 32)
	if err != nil {
		log.Fatalf("error parsing %s from %s: %s", name, full, err)
	}

	if i < 0 || i > 65535 {
		log.Fatalf("%s %d is out of range", name, i)
	}

	return int32(i)
}

func parsePorts(ports []string) []port {
	var ret []port
	for _, p := range ports {
		parsed := port{}
		idx := strings.Index(p, ":")
		if idx != -1 {
			parsed.listenPort = parseInt(p, p[:idx], "listen port")
			parsed.servicePort = parseInt(p, p[idx+1:], "service port")
		} else {
			parsed.listenPort = parseInt(p, p, "listen port")
			parsed.servicePort = parsed.listenPort
		}
		ret = append(ret, parsed)
	}
	return ret
}
