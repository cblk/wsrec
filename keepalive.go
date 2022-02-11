package wsrec

import (
	"sync"
	"time"

	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
)

type keepAliveResponse struct {
	lastResponse time.Time
	sync.RWMutex
}

func (k *keepAliveResponse) setLastResponse() {
	k.Lock()
	defer k.Unlock()

	k.lastResponse = time.Now()
}

func (k *keepAliveResponse) getLastResponse() time.Time {
	k.RLock()
	defer k.RUnlock()

	return k.lastResponse
}

func (rc *RecConn) writeControlPingMessage() error {
	rc.Lock()
	defer rc.Unlock()
	if rc.Conn == nil {
		return ErrNotConnected
	}
	return rc.Conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(10*time.Second))
}

func (rc *RecConn) keepAlive() {
	if rc.response == nil {
		rc.response = new(keepAliveResponse)
	}
	rc.Lock()
	rc.Conn.SetPongHandler(func(msg string) error {
		rc.response.setLastResponse()
		return nil
	})
	rc.Unlock()
	go rc.ticker()
}

func (rc *RecConn) ticker() {
	ticker := time.NewTicker(rc.getKeepAliveTimeout())
	defer ticker.Stop()
	for {
		if rc.GetConnectionState() != Connected {
			continue
		}
		if err := rc.writeControlPingMessage(); err != nil {
			log.Infoln(err)
		}
		<-ticker.C
		if time.Since(rc.response.getLastResponse()) > rc.getKeepAliveTimeout() {
			log.Infoln("keep alive timeout")
			rc.Restart()
			return
		}
	}
}
