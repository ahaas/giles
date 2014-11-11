package main

import (
	"flag"
	"github.com/gorilla/mux"
	"github.com/gtfierro/giles/giles"
	"net/http"
	"os"
	"runtime"
	"runtime/pprof"
	"strconv"
	"time"
)

// logging config
var log = logging.MustGetLogger("archiver")
var format = "%{color}%{level} %{time:Jan 02 15:04:05} %{shortfile}%{color:reset} ▶ %{message}"

// config flags
var cpuprofile = flag.String("cpuprofile", "", "write cpu profile to file")
var memprofile = flag.String("memprofile", "", "write memory profile to this file")
var archiverport = flag.Int("port", 8079, "archiver service port")
var readingdbip = flag.String("rdbip", "localhost", "ReadingDB IP address")
var readingdbport = flag.Int("rdbport", 4242, "ReadingDB Port")
var mongoip = flag.String("mongoip", "localhost", "MongoDB IP address")
var mongoport = flag.Int("mongoport", 27017, "MongoDB Port")
var tsdbstring = flag.String("tsdb", "readingdb", "Type of timeseries database to use: 'readingdb' or 'quasar'")
var tsdbkeepalive = flag.Int("keepalive", 30, "Number of seconds to keep TSDB connection alive per stream for reads")
var benchmarktimer = flag.Int("benchmark", 60, "Number of seconds to benchmark before quitting and writing profiles")

func main() {
	flag.Parse()
	log.Notice("Serving on port %v", *archiverport)
	log.Notice("ReadingDB server %v", *readingdbip)
	log.Notice("ReadingDB port %v", *readingdbport)
	log.Notice("Mongo server %v", *mongoip)
	log.Notice("Mongo port %v", *mongoport)
	log.Notice("Using TSDB %v", *tsdbstring)
	log.Notice("TSDB Keepalive %v", *tsdbkeepalive)

	/** Configure CPU profiling */
	if *cpuprofile != "" {
		log.Notice("Benchmarking for %v seconds", *benchmarktimer)
		f, err := os.Create(*cpuprofile)
		if err != nil {
			log.Fatal(err)
		}
		f2, err := os.Create("blockprofile.db")
		if err != nil {
			log.Fatal(err)
		}
		pprof.StartCPUProfile(f)
		runtime.SetBlockProfileRate(1)
		defer runtime.SetBlockProfileRate(0)
		defer pprof.Lookup("block").WriteTo(f2, 1)
		defer pprof.StopCPUProfile()
	}
	republisher = NewRepublisher()

	/** connect to Metadata store*/
	store = NewStore(*mongoip, *mongoport)
	if store == nil {
		log.Fatal("Error connection to MongoDB instance")
	}

	switch *tsdbstring {
	case "readingdb":
		/** connect to ReadingDB */
		tsdb = NewReadingDB(*readingdbip, *readingdbport, *tsdbkeepalive)
		if tsdb == nil {
			log.Fatal("Error connecting to ReadingDB instance")
		}
	case "quasar":
		log.Fatal("quasar")
	default:
		log.Fatal(*tsdbstring, " is not a valid timeseries database")
	}

	runtime.GOMAXPROCS(runtime.NumCPU())

	a := NewArchiver(tsdb, store, "0.0.0.0:"+strconv.Itoa(*archiverport))
	go a.ServeHTTP()

	//go periodicCall(1*time.Second, status) // status from stats.go
	log.Notice("...connected!")
	idx := 0
	for {
		time.Sleep(5 * time.Second)
		idx += 5
		if idx == *benchmarktimer {
			if *memprofile != "" {
				f, err := os.Create(*memprofile)
				if err != nil {
					log.Panic(err)
				}
				pprof.WriteHeapProfile(f)
				f.Close()
				return
			}
			if *cpuprofile != "" {
				return
			}
		}
	}
	//log.Panic(srv.ListenAndServe())

}
