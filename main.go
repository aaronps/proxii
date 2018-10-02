package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

const (
	defaultListenPort = "localhost:8080"
	proxiiVersion     = "0.4.0"
)

func main() {
	log.Print("Proxii V.", proxiiVersion)

	listenAddr := defaultListenPort
	if len(os.Args) > 1 {
		listenAddr = os.Args[1]
	}

	ps, err := newProxii(listenAddr)
	if err != nil {
		log.Fatalf("Cannot create Proxii: %v", err)
	}

	log.Printf("Listenin on port: %d", ps.listener.Addr())

	ps.serve()
}

type proxii struct {
	requestCounter uint64
	listener       net.Listener
	dialer         *net.Dialer
	server         *http.Server
	client         *http.Client
}

func newProxii(addr string) (*proxii, error) {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}

	dialer := &net.Dialer{Timeout: 4000 * time.Millisecond}

	client := &http.Client{
		Transport: &http.Transport{
			DialContext: dialer.DialContext,
		},
		Timeout: time.Millisecond * 10000,
	}

	result := &proxii{
		listener: listener,
		client:   client,
		dialer:   dialer,
	}

	result.server = &http.Server{Handler: result}

	return result, nil
}

func (p *proxii) serve() error {
	return http.Serve(p.listener, p)
}

func (p *proxii) close() error {
	return p.server.Close()
}

func (p *proxii) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	requestID := atomic.AddUint64(&p.requestCounter, 1)

	log.Print(requestID, "| Method: ", request.Method, " URL: ", request.URL, " Proto: ", request.Proto, " Host: ", request.Host)

	if request.Method == "CONNECT" {
		handleConnect(requestID, p.dialer, response, request)
	} else if strings.ToLower(request.Header.Get("Connection")) == "upgrade" && strings.ToLower(request.Header.Get("upgrade")) == "websocket" {
		handleWebsocket(requestID, p.dialer, response, request)
	} else {
		handleRequest(requestID, p.client, response, request)
	}

	log.Print(requestID, "| Request end: ")
}

func handleConnect(requestID uint64, dialer *net.Dialer, response http.ResponseWriter, request *http.Request) {
	rh := response.Header()

	conn, err := dialer.Dial("tcp", request.Host)
	if err != nil {
		if neterror, ok := err.(*net.OpError); ok {
			switch realerror := neterror.Err.(type) {
			case *net.DNSError:
				log.Print(requestID, "| Connect error(dns): ", realerror.Error())

			default:
				log.Print(requestID, "| Connect error:(net)", realerror.Error())
			}
		} else {
			log.Print(requestID, "| Connect error(gen): ", err)
		}
		rh.Add("Content-Type", "text/plain")
		response.WriteHeader(http.StatusBadGateway)
		fmt.Fprintf(response, "Connect error: %v", err)
		return
	}

	defer conn.Close()

	hijacker, _ := response.(http.Hijacker)
	clientConn, clientRw, err := hijacker.Hijack()

	defer clientConn.Close()

	log.Print(requestID, "| Connect success")
	clientRw.WriteString("HTTP/1.1 200 Connection established\r\n\r\n")
	clientRw.Flush()

	go io.Copy(conn, clientRw)
	io.Copy(clientRw, conn)
}

func handleRequest(requestID uint64, client *http.Client, response http.ResponseWriter, request *http.Request) {
	rh := response.Header()

	// the next two are to support transparent proxy function
	if request.URL.Scheme == "" {
		request.URL.Scheme = "http"
	}

	if request.URL.Host == "" {
		request.URL.Host = request.Host
	}

	creq, err := http.NewRequest(request.Method, request.URL.String(), request.Body)
	if err != nil {
		log.Print(requestID, "| NewRequest error: ", err)
		rh.Add("Content-Type", "text/plain")
		response.WriteHeader(http.StatusBadGateway)
		fmt.Fprintf(response, "New request error: %v", err)
		return
	}

	creq.Header = request.Header
	delete(creq.Header, "Proxy-Connection")

	//client := &http.Client{}
	cresp, err := client.Do(creq)
	if err != nil {
		log.Print(requestID, "| Request error: ", err)
		rh.Add("Content-Type", "text/plain")
		response.WriteHeader(http.StatusBadGateway)
		fmt.Fprintf(response, "Request error: %v", err)
		return
	}

	// we are expected to close body
	defer cresp.Body.Close()

	// copy response headers
	for key, value := range cresp.Header {
		rh[key] = value
	}

	// ignoring errors from this point
	response.WriteHeader(cresp.StatusCode)

	io.Copy(response, cresp.Body)
}

func handleWebsocket(requestID uint64, dialer *net.Dialer, response http.ResponseWriter, request *http.Request) {
	rh := response.Header()

	conn, err := dialer.Dial("tcp", request.Host)
	if err != nil {
		if neterror, ok := err.(*net.OpError); ok {
			switch realerror := neterror.Err.(type) {
			case *net.DNSError:
				log.Print(requestID, "| WSConnect error(dns): ", realerror.Error())

			default:
				log.Print(requestID, "| WSConnect error:(net)", realerror.Error())
			}
		} else {
			log.Print(requestID, "| WSConnect error(gen): ", err)
		}
		rh.Add("Content-Type", "text/plain")
		response.WriteHeader(http.StatusBadGateway)
		fmt.Fprintf(response, "WSConnect error: %v", err)
		return
	}

	defer conn.Close()

	request.URL.Scheme = "ws"
	request.URL.Host = request.Host

	// well, theoretically httputil.DumpRequest shouldn't be used... but it works
	reconstructedResponse, err := httputil.DumpRequest(request, false)
	if err != nil {
		log.Print("cannot reconstruct:", err)
		rh.Add("Content-Type", "text/plain")
		response.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(response, "WSConnect dump error: %v", err)
		return
	}

	hijacker, _ := response.(http.Hijacker)
	clientConn, clientRw, err := hijacker.Hijack()

	defer clientConn.Close()

	log.Print(requestID, "| WSConnect success")

	conn.Write(reconstructedResponse)

	log.Print(requestID, "| WSConnect Wrote header")

	// handshake and such handled directly between interested parties

	go io.Copy(conn, clientRw)
	io.Copy(clientRw, conn)
}
