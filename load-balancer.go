package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ProxyConn wraps a proxy connection pair
type ProxyConn struct {
	FrontConn net.Conn
	BackConn  net.Conn
	Name      string
	ForceQuit chan bool
}

// BackendServer identifies a backend server
type BackendServer struct {
	Host string
	Port int
}

// LoadBalancer is a single instance of load balancer
type LoadBalancer struct {
	BackendServers []*BackendServer
	ListenPort     int
	ConnPairs      map[*ProxyConn]bool
	Mutex          sync.Mutex
	backendIndex   int
	listener       net.Listener
	chanExit       chan bool
}

func (proxyConn *ProxyConn) String() string {
	return fmt.Sprintf("proxy connection %v <-> %v (%v)",
		proxyConn.FrontConn.RemoteAddr(),
		proxyConn.BackConn.RemoteAddr(),
		proxyConn.Name)
}

// NewLoadBalancer constructs a new load balancer
func NewLoadBalancer(listenPort int, backendStr string) *LoadBalancer {
	loadBalancer := LoadBalancer{
		ListenPort:   listenPort,
		ConnPairs:    make(map[*ProxyConn]bool),
		backendIndex: 0,
		chanExit:     make(chan bool, 1),
	}
	backends := strings.Split(backendStr, ",")
	for _, backend := range backends {
		fields := strings.Split(backend, ":")
		if len(fields) == 2 {
			host := fields[0]
			portStr := fields[1]
			port, err := strconv.Atoi(portStr)
			if err == nil {
				log.Printf("Adding backend %v:%v\n", host, port)
				loadBalancer.BackendServers = append(loadBalancer.BackendServers, &BackendServer{
					Host: host,
					Port: port,
				})
			}
		}
	}
	return &loadBalancer
}

// GetConnPairs gets connection pairs information
func (lb *LoadBalancer) GetConnPairs() []*LBConnection {
	var result []*LBConnection

	lb.Mutex.Lock()
	for connPair := range lb.ConnPairs {
		x := LBConnection{
			BackendAddress:  connPair.BackConn.RemoteAddr().String(),
			FrontendAddress: connPair.FrontConn.RemoteAddr().String(),
		}
		result = append(result, &x)
	}
	lb.Mutex.Unlock()
	return result
}

// Start starts the frontend listener. It launches a go routine on success
func (lb *LoadBalancer) Start() error {
	serverAddr, err := net.ResolveTCPAddr("tcp", fmt.Sprintf(":%v", lb.ListenPort))
	if err != nil {
		return err
	}

	log.Printf("Listening on %v\n", serverAddr)
	lb.listener, err = net.ListenTCP("tcp", serverAddr)
	if err != nil {
		return err
	}

	go func() {

		for {
			conn, err := lb.listener.Accept()
			if err != nil {
				log.Printf("Failed to accept %v. Breaking", lb)
				break
			}
			//log.Printf("Received frontend connection from %v", conn.RemoteAddr())
			connPair := &ProxyConn{
				FrontConn: conn,
				ForceQuit: make(chan bool),
			}
			go lb.HandleConnectionPair(connPair)
		}
		// inform that we are done
		lb.chanExit <- true
	}()
	return nil
}

// Stop all existing connections
func (lb *LoadBalancer) Stop() {
	// Stop the listener first
	lb.listener.Close()

	// Tell all existing connections to terminate
	lb.Mutex.Lock()
	for connPair := range lb.ConnPairs {
		connPair.ForceQuit <- true
	}
	lb.Mutex.Unlock()
	lb.WaitFinish()
}

// WaitFinish waits for the listener goroutine to finish
func (lb *LoadBalancer) WaitFinish() {
	<-lb.chanExit
}

// Connect to the backend server with timeout 100ms
func (server *BackendServer) Connect() (net.Conn, error) {
	remoteAddr := server.Address()
	connTimeout, _ := time.ParseDuration("2s")
	return net.DialTimeout("tcp", remoteAddr, connTimeout)
}

// Address returns a TCP address for connection.
func (server *BackendServer) Address() string {
	var remoteAddr string
	if strings.Contains(server.Host, ":") {
		// IPv6
		remoteAddr = fmt.Sprintf("[%v]:%v", server.Host, server.Port)
	} else {
		remoteAddr = fmt.Sprintf("%v:%v", server.Host, server.Port)
	}

	return remoteAddr
}

func (server *BackendServer) String() string {
	return fmt.Sprintf("%v", server.Address())
}

// ConnectToBackend finds a reachable backend, and returns the connection
func (lb *LoadBalancer) ConnectToBackend() (*BackendServer, net.Conn) {
	lb.Mutex.Lock()
	tryIndex := lb.backendIndex
	lb.Mutex.Unlock()

	for count := len(lb.BackendServers); count > 0; count-- {
		i := tryIndex % len(lb.BackendServers)
		b := lb.BackendServers[i]
		tryIndex++

		c, err := b.Connect()
		if err == nil {
			lb.Mutex.Lock()
			lb.backendIndex = tryIndex
			lb.Mutex.Unlock()
			return b, c
		}
	}
	return nil, nil
}

// HandleConnectionPair handles a connection pair. It blocks, run it in a goroutine
func (lb *LoadBalancer) HandleConnectionPair(connPair *ProxyConn) {
	backend, backConn := lb.ConnectToBackend()
	if backend == nil {
		// we do not have a usable backend
		log.Printf("Failed to find a backend server")
		connPair.FrontConn.Close()
		return
	}

	connPair.BackConn = backConn
	connPair.Name = backend.String()
	//log.Printf("Starting %v", connPair)
	// Good, now we have both connection ready. Do book keeping
	lb.Mutex.Lock()
	lb.ConnPairs[connPair] = true
	lb.Mutex.Unlock()

	// start bi-directional proxy
	closeChan := make(chan bool, 2)
	go func() {
		io.Copy(connPair.FrontConn, connPair.BackConn)
		// EOF or error, close
		closeChan <- true
	}()

	go func() {
		io.Copy(connPair.BackConn, connPair.FrontConn)
		// EOF or error, close
		closeChan <- true
	}()

	// wait until either direction closes
	chanClosed := 0
	select {
	case <-closeChan:
		chanClosed++
	case <-connPair.ForceQuit:
	}

	//log.Printf("Terminating %v", connPair)
	// one direction finishes. Close both.
	connPair.BackConn.Close()
	connPair.FrontConn.Close()

	if chanClosed == 0 {
		<-closeChan
	}
	<-closeChan

	close(closeChan)

	// delete it from the set
	lb.Mutex.Lock()
	delete(lb.ConnPairs, connPair)
	lb.Mutex.Unlock()
}
