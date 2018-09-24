package main

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/gorilla/websocket"
	"io"
	"io/ioutil"
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

func TestTestingWebServer(t *testing.T) {
	testServer, err := newTestingWebServer()
	if err != nil {
		t.Fatal("TestingWebServer cannot start", err)
		return
	}

	defer testServer.close()

	client := &http.Client{}

	var tests []requestConfig = []requestConfig{
		{"GET", testServer.urlBase + "/get", "", nil, http.StatusOK, []byte("GET /get")},
		{"PUT", testServer.urlBase + "/put", "text/plain", strings.NewReader("putbody"), http.StatusOK, []byte("PUT /put: putbody")},
		{"ZZZ", testServer.urlBase + "/zzz", "", nil, http.StatusNotImplemented, nil},
		{"POST", testServer.urlBase + "/form", "application/x-www-form-urlencoded", strings.NewReader(url.Values{"k": {"v"}}.Encode()), http.StatusOK, []byte(`POST /form: {"k":["v"]}`)},
	}

	for _, test := range tests {
		doRequest(t, client, &test)
	}

	// now test websocket

	wscon, _, err := websocket.DefaultDialer.Dial(strings.Replace(testServer.urlBase, "http", "ws", 1)+"/ws", nil)
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

}
