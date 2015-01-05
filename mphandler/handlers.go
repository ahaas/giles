package mphandler

import (
	"github.com/gtfierro/giles/archiver"
	"github.com/op/go-logging"
	"github.com/ugorji/go/codec"
	"net"
	"os"
	"reflect"
)

var log = logging.MustGetLogger("mphandler")
var format = "%{color}%{level} %{time:Jan 02 15:04:05} %{shortfile}%{color:reset} ▶ %{message}"
var logBackend = logging.NewLogBackend(os.Stderr, "", 0)
var mh codec.MsgpackHandle

func Handle(a *archiver.Archiver) {
	log.Notice("Handling MsgPack")
}

func ServeTCP(a *archiver.Archiver, tcpaddr *net.TCPAddr) {
	mh.MapType = reflect.TypeOf(map[string]interface{}(nil))
	listener, err := net.ListenTCP("tcp", tcpaddr)
	if err != nil {
		log.Error("Error on listening: %v", err)
	}
	var v interface{} // value to decode/encode into

	go func() {
		for {
			buf := make([]byte, 1024)
			conn, err := listener.Accept()
			if err != nil {
				log.Error("Error accepting connection: %v", err)
			}
			n, _ := conn.Read(buf)
			dec := codec.NewDecoderBytes(buf[:n], &mh)
			err = dec.Decode(&v)
			AddReadings(a, v.(map[string]interface{}))
		}
	}()
}

func AddReadings(a *archiver.Archiver, input map[string]interface{}) {
	apikey := string(input["key"].([]uint8))
	ret := map[string]*archiver.SmapMessage{}
	for path, md := range input {
		m, ok := md.(map[string]interface{})
		if !ok {
			continue
		}
		if readings, found := md.(map[string]interface{})["Readings"]; found {
			log.Debug("readings %v", readings, len(readings.([]interface{})))
			uuid := string(m["uuid"].([]uint8))
			sm := &archiver.SmapMessage{Path: path,
				UUID:       uuid,
				Metadata:   m["Metadata"].(map[string]interface{}),
				Properties: m["Properties"].(map[string]interface{})}
			sr := &archiver.SmapReading{UUID: uuid}
			srs := make([][]interface{}, len(readings.([]interface{})))
			for idx, smr := range readings.([]interface{}) {
				if value, ok := smr.([]interface{})[1].(float64); !ok {
					srs[idx] = []interface{}{smr.([]interface{})[0].(uint64), smr.([]interface{})[1].(int64)}
				} else {
					srs[idx] = []interface{}{smr.([]interface{})[0].(uint64), value}
				}
			}
			sr.Readings = srs
			sm.Readings = sr
			ret[path] = sm
		}
	}
	a.AddData(ret, apikey)
}
