package main

import (
	"encoding/json"
	"fmt"
	"github.com/gorilla/websocket"
	"io/ioutil"
	"net"
	"net/http"
	"strings"
	"time"
)

type testingWebServer struct {
	urlBase           string
	server            *http.Server
	websocketUpgrader websocket.Upgrader
}

func newTestingWebServer() (*testingWebServer, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}

	newServer := &testingWebServer{urlBase: "http://" + listener.Addr().String()}
	httpServer := &http.Server{Handler: newServer}

	newServer.server = httpServer

	newServer.websocketUpgrader.ReadBufferSize = 1024
	newServer.websocketUpgrader.WriteBufferSize = 1024

	go httpServer.Serve(listener)

	return newServer, nil
}

func (tsrv *testingWebServer) close() {
	tsrv.server.Close()
}

func (tsrv *testingWebServer) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	switch request.Method {
	case "GET":
		if strings.ToLower(request.Header.Get("Connection")) == "upgrade" && strings.ToLower(request.Header.Get("Upgrade")) == "websocket" {
			conn, err := tsrv.websocketUpgrader.Upgrade(response, request, nil)
			if err != nil {
				// gorilla/websocket already replied the to the client
				println("WebSocket upgrader error:", err)
				return
			}

			defer conn.Close()

			for {
				messageType, message, err := conn.ReadMessage()
				if err != nil {
					if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
						println("WebSocket ReadMessage error:", err)
					}
					break
				}

				err = conn.WriteMessage(messageType, message)
				if err != nil {
					println("WebSocket WriteMessage error:", err)
					break
				}
			}

			return
		} else if request.RequestURI == "/notfound" {
			response.WriteHeader(http.StatusNotFound)
		} else if request.RequestURI == "/wait" {
			time.Sleep(2 * time.Second)
		} else if request.RequestURI == "/close" {
			//response.WriteHeader(http.StatusCreated)

			hijacker, _ := response.(http.Hijacker)

			clientConn, clientRw, err := hijacker.Hijack()
			if err == nil {
				clientRw.WriteString("HTTP/1.1 200 Ok\r\n")
				clientRw.Flush()
				clientConn.Close()
			} else {
				fmt.Println("Couldn't hijack: ", err)
			}
		} else {
			fmt.Fprintf(response, "GET %s", request.RequestURI)
		}

	case "PUT":
		var bodyString string

		body, err := ioutil.ReadAll(request.Body)
		if err != nil {
			bodyString = fmt.Sprintf("Cannot read body: %v", err)
		} else {
			bodyString = string(body)
		}

		fmt.Fprintf(response, "PUT %s: %s", request.RequestURI, bodyString)

	case "POST":

		switch request.Header.Get("content-type") {
		case "application/x-www-form-urlencoded":
			err := request.ParseForm()
			if err != nil {
				fmt.Fprintf(response, "Cannot ParseForm: %v", err)
				return
			}

			jsonform, err := json.Marshal(request.PostForm)
			if err != nil {
				fmt.Fprintf(response, "Cannot Marshall json: %v", err)
				return
			}

			fmt.Fprintf(response, "POST %s: %s", request.RequestURI, string(jsonform))

		case "multipart/form-data":
			request.ParseMultipartForm(100000)

		default:
			body, err := ioutil.ReadAll(request.Body)
			if err != nil {
				fmt.Fprintf(response, "Cannot read body: %v", err)
				return
			}

			fmt.Fprintf(response, "POST %s: %s", request.RequestURI, string(body))
		}

	default:
		response.WriteHeader(http.StatusNotImplemented)
	}
}
