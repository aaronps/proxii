package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"sync/atomic"
)

const (
	defaultListenPort = "localhost:8080"
	proxiiVersion     = "0.2.2"
)

func main() {
	log.Print("Proxii V.", proxiiVersion)

	listenAddr := defaultListenPort
	if len(os.Args) > 1 {
		listenAddr = os.Args[1]
	}

	log.Print("Listen on ", listenAddr)

	http.ListenAndServe(listenAddr, &proxii{})
}

type proxii struct {
	requestCounter uint64
}

func (p *proxii) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	requestID := atomic.AddUint64(&p.requestCounter, 1)

	log.Print(requestID, "| Method: ", request.Method, " URL: ", request.URL, " Proto: ", request.Proto, " Host: ", request.Host)

	if request.Method != "CONNECT" {

		rh := response.Header()

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

		client := &http.Client{}
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

	} else {
		rh := response.Header()

		conn, err := net.Dial("tcp", request.Host)
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

	log.Print(requestID, "| Request end: ")
}
