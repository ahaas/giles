package archiver

import (
	"net"
	"sync"
	"time"
)

type connection struct {
	conn *net.Conn
	In   chan *[]byte
}

// Giles can create a connection to readingdb for each UUID representing a timeseries. To avoid
// unnecessarily reopening connections, it maintains a map of UUIDs to watchdog goroutines.
// Each goroutine has a timeout (defaults to 30s) -- if it does not receive a pending write
// for its UUID within the time out, it closes the connection to readingdb, and refreshes
// the timout when it receives a reading.
//
// Using the pool.ConnectionMap interface is by no means necessary, but it can help with
// the parallelization of writes to the timeseries database
type ConnectionMap struct {
	sync.Mutex
	streams   map[string]*connection
	keepalive int
}

func NewConnectionMap(connectionkeepalive int) *ConnectionMap {
	return &ConnectionMap{streams: map[string]*connection{}, keepalive: connectionkeepalive}
}

// Sends data to the specified timeseries database instance. Assumes the
// data is associated with the accompanying uuid so it can reuse that connection.
// Calling cm.Add will create a connection if there isn't one, or send data
// along a previously existing connection.
func (cm *ConnectionMap) Add(uuid string, data *[]byte, tsdb TSDB) {
	if conn := cm.streams[uuid]; conn != nil {
		conn.In <- data
	} else {
		cm.Lock()
		defer cm.Unlock()
		log.Notice("new conn for %v", uuid)
		// start new watchdog
		c, err := tsdb.GetConnection()
		if err != nil {
			log.Panic("Error connecting to TSDB")
		}
		conn = &connection{conn: &c, In: make(chan *[]byte)}
		cm.streams[uuid] = conn
		go cm.watchdog(uuid)
		conn.In <- data
	}
}

func (cm *ConnectionMap) watchdog(uuid string) {
	var timeout <-chan time.Time
	timer := time.NewTimer(time.Duration(cm.keepalive) * time.Second)
	timeout = timer.C
	conn := cm.streams[uuid]
	for {
		if conn == nil {
			return
		}
		select {
		case data := <-conn.In:
			timer.Reset(time.Duration(cm.keepalive) * time.Second)
			timeout = timer.C
			//TODO: fix this reference?
			//pendingwritescounter.Mark()
			_, err := (*conn.conn).Write(*data)
			if err != nil {
				log.Error("Error writing data to ReadingDB", err)
			}
		case <-timeout:
			log.Notice("timeout for %v", uuid)
			cm.Lock()
			(*conn.conn).Close()
			close(conn.In)
			delete(cm.streams, uuid)
			cm.Unlock()
			return
		}
	}
}

func (cm *ConnectionMap) LiveConnections() int {
	return len(cm.streams)
}
