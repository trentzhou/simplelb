package main

import (
	"os"
	"strconv"
)

func main() {
	backendsStr := os.Getenv("BACKENDS")
	listenPortStr := os.Getenv("LISTEN_PORT")

	listenPort, err := strconv.Atoi(listenPortStr)
	if err == nil {
		lb := NewLoadBalancer(listenPort, backendsStr)
		err = lb.Start()
		lb.WaitFinish()
	}
}
