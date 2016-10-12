package main

import (
	"flag"
	"fmt"
	"math"
	"net/http"
	"os"
	"strings"
	"time"

	_ "net/http/pprof"

	"github.com/ncabatoff/dbms_exporter/common"
	"github.com/ncabatoff/dbms_exporter/config"
	"github.com/ncabatoff/dbms_exporter/db"
	"github.com/ncabatoff/dbms_exporter/recipes"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/log"
)

var Version string = "0.0.1"

var (
	listenAddress = flag.String(
		"web.listen-address", ":9113",
		"Address to listen on for web interface and telemetry.",
	)
	metricPath = flag.String(
		"web.telemetry-path", "/metrics",
		"Path under which to expose metrics.",
	)
	queriesPath = flag.String(
		"queryfile", "",
		"File with queries to run.",
	)
	onlyDumpMaps = flag.Bool(
		"dumpmaps", false,
		"Do not run, simply dump the maps.",
	)
	driver = flag.String(
		"driver", "postgres",
		"DB driver to user, one of ("+strings.Join(db.Drivers(), ",")+
			"); sybase is the same as freetds except for the prefix of generated metrics)",
	)
	persistentConnection = flag.Bool(
		"persistent.connection", false,
		"keep a DB connection open rather than opening a new one for each scrape",
	)
)

// Metric name parts.
const (
	// Subsystems.
	exporter = "exporter"
)

// landingPageFmt contains the HTML served at '/'.
// TODO: Make this nicer and more informative.
var landingPageFmt = `<html>
<head><title>%s exporter</title></head>
<body>
<h1>%s exporter</h1>
<p><a href='` + *metricPath + `'>Metrics</a></p>
</body>
</html>
`

// Stores the prometheus metric description which a given column will be mapped
// to by the collector
type MetricMap struct {
	discard    bool                              // Should metric be discarded during mapping?
	vtype      prometheus.ValueType              // Prometheus valuetype
	desc       *prometheus.Desc                  // Prometheus descriptor
	conversion func(interface{}) (float64, bool) // Conversion function to turn DB result into float64
	fixedval   string
}

// Groups metric maps under a shared set of labels
type MetricMapNamespace struct {
	labels         []string             // Label names for this namespace
	columnMappings map[string]MetricMap // Column mappings in this namespace
}

func makeDescMap(metricName string, resultMap recipes.ResultMap, recipe recipes.MetricQueryRecipe) MetricMapNamespace {
	thisMap := make(map[string]MetricMap)

	// Get the constant labels
	var variableLabels []string
	var constLabels = make(prometheus.Labels)
	for columnName, columnMapping := range resultMap {
		if columnMapping.Usage == common.LABEL {
			variableLabels = append(variableLabels, columnName)
		} else if columnMapping.Usage == common.FIXED {
			constLabels[columnName] = columnMapping.Fixedval
		}
	}

	newDesc := func(colName, desc string) *prometheus.Desc {
		return prometheus.NewDesc(fmt.Sprintf("%s_%s", metricName, colName), desc, variableLabels, constLabels)
	}

	for columnName, columnMapping := range resultMap {
		switch columnMapping.Usage {
		case common.DISCARD, common.LABEL:
			thisMap[columnName] = MetricMap{
				discard: true,
			}
		case common.COUNTER:
			regexp := columnMapping.Regexp
			thisMap[columnName] = MetricMap{
				vtype: prometheus.CounterValue,
				desc:  newDesc(columnName, columnMapping.Description),
				conversion: func(in interface{}) (float64, bool) {
					return db.ToFloat64(in, regexp)
				},
			}
		case common.GAUGE:
			regexp := columnMapping.Regexp
			thisMap[columnName] = MetricMap{
				vtype: prometheus.GaugeValue,
				desc:  newDesc(columnName, columnMapping.Description),
				conversion: func(in interface{}) (float64, bool) {
					return db.ToFloat64(in, regexp)
				},
			}
		case common.MAPPEDMETRIC:
			thisMap[columnName] = MetricMap{
				vtype: prometheus.GaugeValue,
				desc:  newDesc(columnName, columnMapping.Description),
				conversion: func(in interface{}) (float64, bool) {
					text, ok := in.(string)
					if !ok {
						return math.NaN(), false
					}

					val, ok := columnMapping.Mapping[text]
					if !ok {
						return math.NaN(), false
					}
					return val, true
				},
			}
		case common.DURATION:
			fullName := fmt.Sprintf("%s_milliseconds", columnName)
			thisMap[columnName] = MetricMap{
				vtype:      prometheus.GaugeValue,
				desc:       newDesc(fullName, columnMapping.Description),
				conversion: convertDuration,
			}
		}
	}
	return MetricMapNamespace{variableLabels, thisMap}
}

func convertDuration(in interface{}) (float64, bool) {
	var durationString string
	switch t := in.(type) {
	case []byte:
		durationString = string(t)
	case string:
		durationString = t
	default:
		log.Errorln("DURATION conversion metric was not a string")
		return math.NaN(), false
	}

	if durationString == "-1" {
		return math.NaN(), false
	}

	d, err := time.ParseDuration(durationString)
	if err != nil {
		return math.NaN(), false
	}
	return float64(d / time.Millisecond), true
}

// Turn the MetricMap column mapping into a prometheus descriptor mapping.
func makeDescMaps(recipes []recipes.MetricQueryRecipe) map[string]MetricMapNamespace {
	var metricMap = make(map[string]MetricMapNamespace)

	for _, recipe := range recipes {
		namespace := recipe.GetNamespace()

		for _, rm := range recipe.GetResultMaps() {
			if rm.Name == "discard" {
				continue
			}
			metricName := namespace
			if rm.Name != "metrics" {
				metricName = metricName + "_" + rm.Name
			}

			metricMap[metricName] = makeDescMap(metricName, rm.ResultMap, recipe)
		}
	}

	return metricMap
}

type scrapeRequest struct {
	results chan<- prometheus.Metric
	done    chan struct{}
}

// Exporter collects DB metrics. It implements prometheus.Collector.
type Exporter struct {
	dsn                  string
	driver               string
	persistentConnection bool
	conn                 db.Conn
	scrapeChan           chan scrapeRequest
	duration             prometheus.Gauge
	totalScrapes         prometheus.Counter
	errors_total         prometheus.Counter
	open_seconds_total   prometheus.Counter
	query_seconds_total  *prometheus.CounterVec
	metricMap            map[string]MetricMapNamespace
	recipes              []recipes.MetricQueryRecipe
}

// NewExporter returns a new exporter for the provided DSN.
func NewExporter(driver, dsn string, recipes []recipes.MetricQueryRecipe, persistentConn bool) *Exporter {
	return &Exporter{
		driver: driver,
		dsn:    dsn,
		duration: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: driver,
			Subsystem: exporter,
			Name:      "last_scrape_duration_seconds",
			Help:      "Duration of the last scrape of metrics from DB",
		}),
		totalScrapes: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: driver,
			Subsystem: exporter,
			Name:      "scrapes_total",
			Help:      "Total number of times the DB was scraped for metrics.",
		}),
		errors_total: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: driver,
			Subsystem: exporter,
			Name:      "scrape_errors_total",
			Help:      "How many scrapes failed due to an error",
		}),
		open_seconds_total: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: driver,
			Subsystem: exporter,
			Name:      "open_seconds_total",
			Help:      "How much time was consumed opening DB connections",
		}),
		query_seconds_total: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: driver,
			Subsystem: exporter,
			Name:      "query_seconds_total",
			Help:      "How much time was consumed opening DB connections",
		}, []string{"namespace"}),
		metricMap:            makeDescMaps(recipes),
		recipes:              recipes,
		persistentConnection: persistentConn,
		scrapeChan:           make(chan scrapeRequest),
	}
}

// Describe implements prometheus.Collector.
func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	// We cannot know in advance what metrics the exporter will generate
	// from the DB. So we use the poor man's describe method: Run a collect
	// and send the descriptors of all the collected metrics. The problem
	// here is that we need to connect to the DB. If it is currently
	// unavailable, the descriptors will be incomplete. Since this is a
	// stand-alone exporter and not used as a library within other code
	// implementing additional metrics, the worst that can happen is that we
	// don't detect inconsistent metrics created by this exporter
	// itself. Also, a change in the monitored DB instance may change the
	// exported metrics during the runtime of the exporter.

	metricCh := make(chan prometheus.Metric)
	doneCh := make(chan struct{})

	go func() {
		for m := range metricCh {
			ch <- m.Desc()
		}
		close(doneCh)
	}()

	e.Collect(metricCh)
	close(metricCh)
	<-doneCh
}

// Collect implements prometheus.Collector.
func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	req := scrapeRequest{results: ch, done: make(chan struct{})}
	e.scrapeChan <- req
	<-req.done
}

func (e *Exporter) Start() {
	go func() {
		for req := range e.scrapeChan {
			ch := req.results
			e.scrape(ch)

			ch <- e.duration
			ch <- e.totalScrapes
			ch <- e.errors_total
			ch <- e.open_seconds_total
			e.query_seconds_total.Collect(ch)
			req.done <- struct{}{}
		}
	}()
}

func (e *Exporter) scrapeRecipe(ch chan<- prometheus.Metric, conn db.Conn, recipe recipes.MetricQueryRecipe) {
	namespace := recipe.GetNamespace()
	log.Debugln("Querying namespace: ", namespace)
	qstart := time.Now()
	srss, err := recipe.Run(conn)
	e.query_seconds_total.WithLabelValues(namespace).Add(time.Since(qstart).Seconds())

	if err != nil {
		log.Infof("Error running query for %q: %v", namespace, err)
		e.errors_total.Inc()
		if e.conn != nil {
			e.conn.Close()
		}
		e.conn = nil
		return
	}

	rms := recipe.GetResultMaps()
	for i, srs := range srss {
		log.Debugf("handling resultset %d with %d rows", i, len(srs.Rows))
		rm := rms[i]
		// handle the 'discard' scenario by skipping this resultset
		if rm.ShouldSkip() {
			continue
		}
		ns := namespace
		if rm.Name != "metrics" {
			ns = ns + "_" + rm.Name
		}
		e.scrapeResultSet(ch, ns, srs, rm.ResultMap)
	}
}

func (e *Exporter) scrapeResultSet(ch chan<- prometheus.Metric, namespace string, srs db.ScannedResultSet, rm recipes.ResultMap) {
	// Make a lookup map for the column indices
	var columnIdx = make(map[string]int, len(srs.Colnames))
	for i, n := range srs.Colnames {
		columnIdx[n] = i
	}

	for _, row := range srs.Rows {
		// Get the label values for this row
		mapping := e.metricMap[namespace]
		var labels = make([]string, len(mapping.labels))
		for idx, columnName := range mapping.labels {
			labels[idx], _ = db.ToString(row[columnIdx[columnName]])
		}

		// Loop over column names, and match to scan data. Unknown columns
		// will be filled with an untyped metric number *if* they can be
		// converted to float64s. NULLs are allowed and treated as NaN.
		for idx, columnName := range srs.Colnames {
			columnName = strings.Replace(columnName, " ", "_", -1)
			if metricMapping, ok := mapping.columnMappings[columnName]; ok {
				// Is this a metricy metric?
				if metricMapping.discard {
					continue
				}

				value, ok := metricMapping.conversion(row[idx])
				if !ok {
					e.errors_total.Inc()
					log.Errorln("Unexpected error parsing column: ", namespace, columnName, row[idx])
					continue
				}

				// Generate the metric
				ch <- prometheus.MustNewConstMetric(metricMapping.desc, metricMapping.vtype, value, labels...)
			} else {
				log.Debugf("unknown metric %q in namespace %q, labels: %v", columnName, namespace, labels)
				// Unknown metric. Report as untyped if scan to float64 works, else note an error too.
				desc := prometheus.NewDesc(fmt.Sprintf("%s_%s_%s", e.driver, namespace, columnName), fmt.Sprintf("Unknown metric from %s", namespace), nil, nil)

				// Its not an error to fail here, since the values are
				// unexpected anyway.
				value, ok := db.ToFloat64(row[idx], nil)
				if !ok {
					log.Warnln("Unparseable column type - discarding: ", namespace, columnName)
					continue
				}

				ch <- prometheus.MustNewConstMetric(desc, prometheus.UntypedValue, value)
			}
		}
	}
}

func (e *Exporter) scrape(ch chan<- prometheus.Metric) {
	defer func(begun time.Time) {
		e.duration.Set(time.Since(begun).Seconds())
	}(time.Now())

	e.totalScrapes.Inc()

	conn := e.conn

	if conn == db.Conn(nil) {
		start := time.Now()
		var err error
		conn, err = db.Open(e.driver, e.dsn)
		if err != nil {
			log.Infof("Error opening connection to %s database: %v", e.driver, err)
			e.errors_total.Inc()
			return
		}
		if e.persistentConnection {
			e.conn = conn
		} else {
			defer conn.Close()
		}
		e.open_seconds_total.Add(time.Since(start).Seconds())
	}

	for _, recipe := range e.recipes {
		e.scrapeRecipe(ch, conn, recipe)
	}
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, usage)
	}
	flag.Parse()

	if *queriesPath == "" {
		log.Fatalf("-queryfile is a required argument")
	}

	rcps, err := config.ReadRecipesFile(*queriesPath, *driver)
	if err != nil {
		log.Fatalf("error parsing file %q: %v", *queriesPath, err)
	}
	if *driver == "sybase" {
		*driver = "freetds"
	}

	found := false
	for _, d := range db.Drivers() {
		if d == *driver {
			found = true
			break
		}
	}
	if !found {
		log.Fatalf("driver %q not supported in this build", *driver)
	}

	if *onlyDumpMaps {
		recipes.DumpMaps(rcps)
		return
	}

	dsn := os.Getenv("DATA_SOURCE_NAME")
	if len(dsn) == 0 {
		log.Fatal("couldn't find environment variable DATA_SOURCE_NAME")
	}

	exporter := NewExporter(*driver, dsn, rcps, *persistentConnection)
	exporter.Start()
	prometheus.MustRegister(exporter)

	http.Handle(*metricPath, prometheus.Handler())
	landingPage := []byte(fmt.Sprintf(landingPageFmt, *driver, *driver))
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write(landingPage)
	})

	log.Infof("Starting Server: %s", *listenAddress)
	log.Fatal(http.ListenAndServe(*listenAddress, nil))
}

var usage = `
The DATA_SOURCE_NAME enviroment variable specifies connection details.  Examples:
	
  Sybase FreeTDS example (driver=freetds):
	compatibility_mode=sybase;user=myuser;pwd=mypassword;server=myhostname

  PostgreSQL example (driver=postgres):
	postgres://myuser@myhost:5432/?sslmode=disable&dbname=postgres&client_encoding=UTF8
`
