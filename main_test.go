package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/gorilla/websocket"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type requestConfig struct {
	method               string
	url                  string
	contentType          string
	body                 io.Reader
	expectedStatusCode   int
	expectedResponseBody []byte
}

func doRequest(t *testing.T, client *http.Client, r *requestConfig) error {
	request, err := http.NewRequest(r.method, r.url, r.body)
	if err != nil {
		t.Fatal("Cannot create request", err)
		return err
	}

	if r.contentType != "" {
		request.Header.Add("Content-Type", r.contentType)
	}

	response, err := client.Do(request)
	if err != nil {
		t.Errorf("Client request error: %v", err)
		return err
	}

	defer response.Body.Close()

	if r.expectedStatusCode != -1 && response.StatusCode != r.expectedStatusCode {
		errString := fmt.Sprintf("StatusCode(%d) different from expected(%d)", response.StatusCode, r.expectedStatusCode)
		t.Error(errString)
		return errors.New(errString)
	}

	if r.expectedResponseBody != nil {
		responseBody, err := ioutil.ReadAll(response.Body)
		if err != nil {
			t.Error("Couldn't read body", err)
			return err
		}

		if !bytes.Equal(responseBody, r.expectedResponseBody) {
			errString := fmt.Sprintf(`Response body different from expected: "%s" != "%s"`, string(responseBody), string(r.expectedResponseBody))
			t.Error(errString)
			return errors.New(errString)
		}
	}

	return nil
}

func runTests(t *testing.T, urlBase string, httpClient *http.Client, wsDialer *websocket.Dialer) {

	tests := []struct {
		name string
		rc   requestConfig
	}{
		{"testGET", requestConfig{"GET", urlBase + "/get", "", nil, http.StatusOK, []byte("GET /get")}},
		{"testPUT", requestConfig{"PUT", urlBase + "/put", "text/plain", strings.NewReader("putbody"), http.StatusOK, []byte("PUT /put: putbody")}},
		{"testUninplemented", requestConfig{"ZZZ", urlBase + "/zzz", "", nil, http.StatusNotImplemented, nil}},
		{"testPOST", requestConfig{"POST", urlBase + "/form", "application/x-www-form-urlencoded", strings.NewReader(url.Values{"k": {"v"}}.Encode()), http.StatusOK, []byte(`POST /form: {"k":["v"]}`)}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			doRequest(t, httpClient, &test.rc)
		})
	}

	// now test websocket

	t.Run("websocket", func(t *testing.T) {
		wscon, _, err := wsDialer.Dial(strings.Replace(urlBase, "http", "ws", 1)+"/ws", nil)
		if err != nil {
			t.Errorf("WebSocket connection error: %v", err)
			return
		}

		defer wscon.Close()

		testMessage := []byte("Testing Message")

		err = wscon.WriteMessage(websocket.TextMessage, testMessage)
		if err != nil {
			t.Errorf("Websocket Write error: %v", err)
		} else {
			mtype, rmsg, err := wscon.ReadMessage()

			switch {
			case err != nil:
				t.Errorf("Websocket Receive error: %v", err)

			case mtype != websocket.TextMessage:
				t.Errorf("Websocket Received incorrect type: %v", mtype)

			case !bytes.Equal(rmsg, testMessage):
				t.Errorf(`Websocket Received Message is differnt with sent: "%s" != "%s"`, string(rmsg), string(testMessage))
			}
		}
	})
}

func TestTestingWebServer(t *testing.T) {
	testServer, err := newTestingWebServer()
	if err != nil {
		t.Fatal("TestingWebServer cannot start", err)
		return
	}

	defer testServer.close()

	runTests(t, testServer.urlBase, http.DefaultClient, websocket.DefaultDialer)
}

func TestProxii(t *testing.T) {
	testServer, err := newTestingWebServer()
	if err != nil {
		t.Fatal("TestingWebServer cannot start", err)
		return
	}

	defer testServer.close()

	proxii, err := newProxii("127.0.0.1:0")
	if err != nil {
		t.Fatal("Cannot start proxii", err)
		return
	}

	defer proxii.close()

	proxiiUrl, err := url.Parse("http://" + proxii.listener.Addr().String())
	if err != nil {
		t.Fatal("Proxy URL is bad", err)
		return
	}

	defer proxii.close()
	go proxii.serve()

	httpClient := &http.Client{
		Transport: &http.Transport{Proxy: func(request *http.Request) (*url.URL, error) { return proxiiUrl, nil }},
	}

	wsDialer := &websocket.Dialer{
		Proxy: func(request *http.Request) (*url.URL, error) { return proxiiUrl, nil },
	}

	runTests(t, testServer.urlBase, httpClient, wsDialer)

}

func TestTransparentProxii(t *testing.T) {
	testServer, err := newTestingWebServer()
	if err != nil {
		t.Fatal("TestingWebServer cannot start", err)
		return
	}

	defer testServer.close()

	proxii, err := newProxii("127.0.0.1:0")
	if err != nil {
		t.Fatal("Cannot start proxii", err)
		return
	}

	defer proxii.close()
	go proxii.serve()

	dialer := &net.Dialer{}

	httpClient := &http.Client{
		Transport: &http.Transport{DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			con, err := dialer.DialContext(ctx, "tcp", proxii.listener.Addr().String())
			return con, err
		}},
	}

	wsDialer := &websocket.Dialer{
		NetDialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			con, err := dialer.DialContext(ctx, "tcp", proxii.listener.Addr().String())
			return con, err
		},
	}

	runTests(t, testServer.urlBase, httpClient, wsDialer)
}
