package httphandler

import (
	"encoding/json"
	"github.com/gtfierro/giles/archiver"
	"net/http"
)

// Implements the archiver.Subscriber interface for the Republish mechanism
type HTTPSubscriber struct {
	rw     http.ResponseWriter
	notify <-chan bool
}

func NewHTTPSubscriber(rw http.ResponseWriter) *HTTPSubscriber {
	rw.Header().Set("Content-Type", "application/json")
	notify := rw.(http.CloseNotifier).CloseNotify()
	return &HTTPSubscriber{rw: rw, notify: notify}
}

// called when we receive a new message
func (hs HTTPSubscriber) Send(msg *archiver.SmapMessage) {
	towrite := make(map[string]interface{})
	towrite[msg.Path] = archiver.SmapReading{Readings: msg.Readings, UUID: msg.UUID}
	bytes, err := json.Marshal(towrite)
	if err != nil {
		hs.rw.WriteHeader(500)
	} else {
		hs.rw.Write(bytes)
		log.Debug("MSG %v", towrite)
		hs.rw.Write([]byte{'\n', '\n'})
	}
	if flusher, ok := hs.rw.(http.Flusher); ok {
		flusher.Flush()
	}
}
func (hs HTTPSubscriber) SendError(e error) {
	hs.rw.WriteHeader(500)
	hs.rw.Write([]byte(e.Error()))
}

func (hs HTTPSubscriber) GetNotify() <-chan bool {
	return hs.notify
}
