package wsrec

import (
	"errors"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
)

// ErrNotConnected is returned when the application read/writes
// a message and the connection is closed
var ErrNotConnected = errors.New("websocket: not connected")

type ConnectionState int

const (
	Disconnected ConnectionState = iota
	Connected
)

type RecConn struct {
	// RecIntervalMin specifies the initial reconnecting interval,
	// default to 2 seconds
	RecIntervalMin time.Duration
	// RecIntervalMax specifies the maximum reconnecting interval,
	// default to 30 seconds
	RecIntervalMax time.Duration
	// RecIntervalFactor specifies the rate of increase of the reconnection
	// interval, default to 1.5
	RecIntervalFactor float64
	// HandshakeTimeout specifies the duration for the handshake to complete,
	// default to 2 seconds
	HandshakeTimeout time.Duration
	// SubscribeHandler fires after the connection successfully establish.
	SubscribeHandler func() error
	// KeepAliveTimeout is an interval for sending ping/pong messages
	// disabled if 0
	KeepAliveTimeout time.Duration

	connectionState ConnectionState
	url             string
	reqHeader       http.Header
	httpResp        *http.Response
	dialErr         error
	dialer          *websocket.Dialer
	response        *keepAliveResponse
	ready           chan bool
	connecting      chan bool

	*websocket.Conn
	sync.RWMutex
}

// Restart will try to close connection and reconnect.
func (rc *RecConn) Restart() {
	select {
	case rc.connecting <- true:
		rc.Close()
		go rc.connect()
		rc.Wait()
	default:
		rc.Wait()
	}

}

// Wait will block until the connection is established.
func (rc *RecConn) Wait() {
	log.Infoln("Waiting for connection...")
	<-rc.ready
	log.Infoln("Connected!")
}

// Close closes the underlying network connection without
// sending or waiting for a close frame.
func (rc *RecConn) Close() {
	if rc.getConn() != nil {
		rc.Lock()
		_ = rc.Conn.Close()
		rc.Unlock()
	}
	rc.setConnectionState(Disconnected)
}

// Shutdown gracefully closes the connection by sending the websocket.CloseMessage.
// The writeWait param defines the duration before the deadline of the write operation is hit.
func (rc *RecConn) Shutdown(writeWait time.Duration) {
	msg := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")
	err := rc.WriteControl(websocket.CloseMessage, msg, time.Now().Add(writeWait))
	if err != nil && err != websocket.ErrCloseSent {
		// If close message could not be sent, then close without the handshake.
		log.Infof("Shutdown: %v", err)
		rc.Close()
	}
}

// ReadMessage is a helper method for getting a reader
// using NextReader and reading from that reader to a buffer.
//
// If the connection is closed ErrNotConnected is returned
func (rc *RecConn) ReadMessage() (messageType int, message []byte, err error) {
	err = ErrNotConnected
	if rc.GetConnectionState() == Connected {
		messageType, message, err = rc.Conn.ReadMessage()
		if websocket.IsCloseError(err, websocket.CloseNormalClosure) {
			rc.Close()
			return messageType, message, nil
		}
		if err != nil {
			rc.Restart()
		}
	}

	return
}

// WriteMessage is a helper method for getting a writer using NextWriter,
// writing the message and closing the writer.
//
// If the connection is closed ErrNotConnected is returned
func (rc *RecConn) WriteMessage(messageType int, data []byte) error {
	err := ErrNotConnected
	if rc.GetConnectionState() == Connected {
		rc.Lock()
		err = rc.Conn.WriteMessage(messageType, data)
		rc.Unlock()
		if websocket.IsCloseError(err, websocket.CloseNormalClosure) {
			rc.Close()
			return nil
		}
		if err != nil {
			rc.Restart()
		}
	}

	return err
}

// WriteJSON writes the JSON encoding of v to the connection.
//
// See the documentation for encoding/json Marshal for details about the
// conversion of Go values to JSON.
//
// If the connection is closed ErrNotConnected is returned
func (rc *RecConn) WriteJSON(v interface{}) error {
	err := ErrNotConnected
	if rc.GetConnectionState() == Connected {
		rc.Lock()
		err = rc.Conn.WriteJSON(v)
		rc.Unlock()
		if websocket.IsCloseError(err, websocket.CloseNormalClosure) {
			rc.Close()
			return nil
		}
		if err != nil {
			rc.Restart()
		}
	}

	return err
}

// ReadJSON reads the next JSON-encoded message from the connection and stores
// it in the value pointed to by v.
//
// See the documentation for the encoding/json Unmarshal function for details
// about the conversion of JSON to a Go value.
//
// If the connection is closed ErrNotConnected is returned
func (rc *RecConn) ReadJSON(v interface{}) error {
	err := ErrNotConnected
	if rc.GetConnectionState() == Connected {
		err = rc.Conn.ReadJSON(v)
		if websocket.IsCloseError(err, websocket.CloseNormalClosure) {
			rc.Close()
			return nil
		}
		if err != nil {
			rc.Restart()
		}
	}

	return err
}

// GetHTTPResponse returns the http response from the handshake.
// Useful when WebSocket handshake fails,
// so that callers can handle redirects, authentication, etc.
func (rc *RecConn) GetHTTPResponse() *http.Response {
	rc.RLock()
	defer rc.RUnlock()

	return rc.httpResp
}

// GetDialError returns the last dialer error.
// nil on successful connection.
func (rc *RecConn) GetDialError() error {
	rc.RLock()
	defer rc.RUnlock()

	return rc.dialErr
}

// GetConnectionState returns the WebSocket connection state
func (rc *RecConn) GetConnectionState() ConnectionState {
	rc.RLock()
	defer rc.RUnlock()

	return rc.connectionState
}

// Dial creates a new client connection.
// The URL specifies the host and request URI. Use requestHeader to specify
// the origin (Origin), sub protocols (Sec-WebSocket-Protocol) and cookies
// (Cookie). Use GetHTTPResponse() method for the response.Header to get
// the selected sub protocol (Sec-WebSocket-Protocol) and cookies (Set-Cookie).
func (rc *RecConn) Dial(urlStr string, reqHeader http.Header) {
	urlStr, err := rc.parseURL(urlStr)
	if err != nil {
		log.Fatalf("Dial: %v", err)
	}

	// Config
	rc.setURL(urlStr)
	rc.setReqHeader(reqHeader)
	rc.setDefaultRecIntvlMin()
	rc.setDefaultRecIntvlMax()
	rc.setDefaultRecIntvlFactor()
	rc.setDefaultHandshakeTimeout()
	rc.setDefaultDialer(rc.getHandshakeTimeout())
	rc.setDefaultSignal()
	// Connect
	go rc.connect()
	rc.Wait()
}

func (rc *RecConn) sendReadyAll() {
	for {
		select {
		case rc.ready <- true:
		default:
			return
		}
	}
}

func (rc *RecConn) finishConnecting() {
	select {
	case <-rc.connecting:
	default:
		return
	}
}

func (rc *RecConn) connect() {
	b := rc.getBackoff()
	rand.Seed(time.Now().UTC().UnixNano())
	for {
		nextInterval := b.Duration()
		wsConn, httpResp, err := rc.dialer.Dial(rc.url, rc.reqHeader)

		rc.Lock()
		rc.Conn = wsConn
		rc.dialErr = err
		rc.httpResp = httpResp
		if err == nil {
			rc.connectionState = Connected
			rc.sendReadyAll()
			rc.finishConnecting()
		}
		rc.Unlock()

		if err == nil {
			log.Infof("Dial: connection was successfully established with %s", rc.url)
			if rc.hasSubscribeHandler() {
				if err := rc.SubscribeHandler(); err != nil {
					log.Fatalf("Dial: connect handler failed with %s", err.Error())
				}
				log.Infof("Dial: subscription established with %s", rc.url)
			}
			if rc.getKeepAliveTimeout() != 0 {
				rc.keepAlive()
			}
			return
		}

		log.Infoln(err)
		log.Infoln("Dial: will try again in", nextInterval, "seconds.")

		time.Sleep(nextInterval)
	}
}
