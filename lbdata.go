package main

import "fmt"

// Proxy wraps one to one proxy
type Proxy struct {
	UUID        string `json:"uuid"`
	ServiceHost string `json:"service_host"`
	ServicePort int    `json:"service_port"`
	ListenPort  int    `json:"listen_port"`
}

// LBConnection identifies a load balancer connection pair
type LBConnection struct {
	BackendAddress  string `json:"backend_addr"`
	FrontendAddress string `json:"frontend_addr"`
	ServiceUUID     string `json:"service_uuid"`
	LBPort          string `json:"lb_port"`
}

// NodeList is used in POST /nodes
type NodeList struct {
	Nodes []string `json:"nodes"`
}

// Error message
type Error struct {
	Reason string `json:"reason"`
}

func (proxy *Proxy) String() string {
	return fmt.Sprintf("[::]:%v to [%v]:%v", proxy.ListenPort, proxy.ServiceHost, proxy.ServicePort)
}
