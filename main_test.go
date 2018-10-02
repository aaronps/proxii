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
	"reflect"
	"strings"
	"testing"
	"time"
)

type requestConfig struct {
	method               string
	url                  string
	contentType          string
	body                 io.Reader
	expectedStatusCode   int
	expectedResponseBody []byte
	isExpectedError      func(t *testing.T, err error) bool
}

func doRequest(t *testing.T, client *http.Client, r *requestConfig) error {
	request, err := http.NewRequest(r.method, r.url, r.body)
	if err != nil {
		if r.isExpectedError != nil && r.isExpectedError(t, err) {
			return nil
		}
		t.Fatal("Cannot create request", err)
		return err
	}

	if r.contentType != "" {
		request.Header.Add("Content-Type", r.contentType)
	}

	response, err := client.Do(request)
	if err != nil {
		if r.isExpectedError != nil && r.isExpectedError(t, err) {
			return nil
		}
		t.Errorf("Client request error(%v): %v", reflect.TypeOf(err), err)
		return err
	}

	defer response.Body.Close()

	if r.expectedStatusCode != 0 && response.StatusCode != r.expectedStatusCode {
		errString := fmt.Sprintf("StatusCode(%d) different from expected(%d)", response.StatusCode, r.expectedStatusCode)
		t.Error(errString)
		return errors.New(errString)
	}

	if r.expectedResponseBody != nil {
		responseBody, err := ioutil.ReadAll(response.Body)
		if err != nil {
			if r.isExpectedError != nil && r.isExpectedError(t, err) {
				return nil
			}
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

type testData struct {
	name string
	rc   requestConfig
}

func runTests(t *testing.T, urlBase string, httpClient *http.Client, wsDialer *websocket.Dialer, extraTests []testData) {

	timeoutListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Errorf("Cannot start timeoutListener: %v", err)
		return
	}

	defer timeoutListener.Close()

	timeoutUrl := "http://" + timeoutListener.Addr().String()

	closerListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Errorf("Cannot start closerListener: %v", err)
		return
	}

	defer closerListener.Close()

	closerUrl := "http://" + closerListener.Addr().String()

	go func() {
		for {
			s, e := closerListener.Accept()
			if e != nil {
				break
			}

			s.Close()
		}
	}()

	commonTests := []testData{
		{
			"testGET", requestConfig{
				method:               "GET",
				url:                  urlBase + "/get",
				expectedStatusCode:   http.StatusOK,
				expectedResponseBody: []byte("GET /get"),
			},
		},
		{
			"testPUT", requestConfig{
				method:               "PUT",
				url:                  urlBase + "/put",
				contentType:          "text/plain",
				body:                 strings.NewReader("putbody"),
				expectedStatusCode:   http.StatusOK,
				expectedResponseBody: []byte("PUT /put: putbody"),
			},
		},
		{
			"testPOST", requestConfig{
				method:               "POST",
				url:                  urlBase + "/form",
				contentType:          "application/x-www-form-urlencoded",
				body:                 strings.NewReader(url.Values{"k": {"v"}}.Encode()),
				expectedStatusCode:   http.StatusOK,
				expectedResponseBody: []byte(`POST /form: {"k":["v"]}`),
			},
		},
		{
			"testUninplemented", requestConfig{
				method:             "ZZZ",
				url:                urlBase + "/zzz",
				expectedStatusCode: http.StatusNotImplemented,
			},
		},
		{
			"testNotFound", requestConfig{
				method:             "GET",
				url:                urlBase + "/notfound",
				expectedStatusCode: http.StatusNotFound,
			},
		},
		{
			"testConnectionRejected", requestConfig{
				method:             "GET",
				url:                "http://127.0.0.1:1/", // nothing should be listening on this port
				expectedStatusCode: http.StatusBadGateway,
				isExpectedError: func(t *testing.T, err error) bool {
					if urlerr, ok := err.(*url.Error); ok {
						if conerr, ok := urlerr.Err.(*net.OpError); ok {
							return conerr.Op == "dial"
						}
					}
					return false
				},
			},
		},
		{
			"testConnectTimeout", requestConfig{
				method:             "GET",
				url:                "http://10.255.255.1",
				expectedStatusCode: http.StatusBadGateway,
				isExpectedError: func(t *testing.T, err error) bool {
					if urlerr, ok := err.(*url.Error); ok {
						if conerr, ok := urlerr.Err.(*net.OpError); ok {
							if conerr.Op == "dial" {
								if !conerr.Timeout() {
									t.Skipf("Skipping test because connect timeout testing is not reliable, check this: %v", conerr)
								}

								return true
							}

							// rabbit hole: I wanted to test whether it really was a connect timeout, or the network was unreachable,
							// but at the end on different systems might get different error codes, for example, on windows some errors
							// will be the winsock code (100XX), so comparing with syscall.ESOMETHING won't work.
						}
					}
					return false
				},
			},
		},
		{
			"testReceiveTimeout-1", requestConfig{
				method:             "GET",
				url:                timeoutUrl,
				expectedStatusCode: http.StatusBadGateway,
				isExpectedError: func(t *testing.T, err error) bool {
					urlerr, ok := err.(*url.Error)
					return ok && urlerr.Op == "Get" && urlerr.Timeout()
				},
			},
		},
		{
			"testReceiveTimeout-2", requestConfig{
				method:             "GET",
				url:                urlBase + "/wait",
				expectedStatusCode: http.StatusBadGateway,
				isExpectedError: func(t *testing.T, err error) bool {
					urlerr, ok := err.(*url.Error)
					return ok && urlerr.Op == "Get" && urlerr.Timeout()
				},
			},
		},
		{
			"testReceiveClose-1", requestConfig{
				method:             "GET",
				url:                closerUrl,
				expectedStatusCode: http.StatusBadGateway,
				isExpectedError: func(t *testing.T, err error) bool {
					urlerr, ok := err.(*url.Error)
					return ok && urlerr.Op == "Get" && !urlerr.Timeout()
				},
			},
		},
		{
			"testReceiveClose-2", requestConfig{
				method:             "GET",
				url:                urlBase + "/close",
				expectedStatusCode: http.StatusBadGateway,
				isExpectedError: func(t *testing.T, err error) bool {
					urlerr, ok := err.(*url.Error)
					return ok && urlerr.Op == "Get" && !urlerr.Timeout()
				},
			},
		},
	}

	// @todo add test to verify when client disconnects, proxii closes the other end connection too.
	// @todo add test to verify when client "shutdown" write, proxii continues to work normally (sending back response)
	// @todo add "CONNECT" tests
	// @todo add errored WebSocket tests
	for _, testList := range [][]testData{commonTests, extraTests} {
		for _, test := range testList {
			t.Run(test.name, func(t *testing.T) {
				doRequest(t, httpClient, &test.rc)
			})
		}
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

	client := &http.Client{
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout: 1000 * time.Millisecond,
			}).DialContext,
		},
		Timeout: time.Millisecond * 2000,
	}

	runTests(t, testServer.urlBase, client, websocket.DefaultDialer, nil)
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

	runTests(t, testServer.urlBase, httpClient, wsDialer, nil)

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

	runTests(t, testServer.urlBase, httpClient, wsDialer, nil)
}
