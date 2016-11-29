package db

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/common/log"
)

var (
	driversMu sync.RWMutex
	drivers   = make(map[string]dbDriver)
)

type dbDriver interface {
	Open(name string) (dbConn, error)
}

// Drivers returns a sorted list of the names of the registered drivers.
func Drivers() []string {
	driversMu.RLock()
	defer driversMu.RUnlock()
	var list []string
	for name := range drivers {
		list = append(list, name)
	}
	sort.Strings(list)
	return list
}

// Register makes a database driver available by the provided name.
// If Register is called twice with the same name or if driver is nil,
// it panics.
func Register(name string, driver dbDriver) {
	driversMu.Lock()
	defer driversMu.Unlock()
	if driver == nil {
		panic("sql: Register driver is nil")
	}
	if _, dup := drivers[name]; dup {
		panic("sql: Register called twice for driver " + name)
	}
	drivers[name] = driver
}

// dbConn is an internal wrapper for things like database/sql.DB
// and gofreetds.Conn.
type dbConn interface {
	query(string) ([]dbResultSet, error)
	Close() error
}

// dbResultSet is
type dbResultSet interface {
	Next() bool
	Scan(dest ...interface{}) error
	Close() error
	Columns() ([]string, error)
}

type Conn interface {
	Query(string) ([]ScannedResultSet, error)
	Close() error
}

type ScannedResultSet struct {
	Colnames []string
	Rows     [][]interface{}
}

func scanResultSet(rs dbResultSet) (*ScannedResultSet, error) {
	defer rs.Close()

	columnNames, err := rs.Columns()
	if err != nil {
		return nil, err
	}
	srs := &ScannedResultSet{Colnames: columnNames}

	var columnData = make([]interface{}, len(columnNames))
	var scanArgs = make([]interface{}, len(columnNames))
	for i := range columnData {
		scanArgs[i] = &columnData[i]
	}

	for rs.Next() {
		err := rs.Scan(scanArgs...)
		if err != nil {
			return nil, err
		}
		srs.Rows = append(srs.Rows,
			append(make([]interface{}, 0, len(columnNames)), columnData...))
	}
	return srs, nil

}

func scanResultSets(rss []dbResultSet) ([]ScannedResultSet, error) {
	srss := make([]ScannedResultSet, 0, len(rss))
	for i, rs := range rss {
		srs, err := scanResultSet(rs)
		if err != nil {
			return nil, fmt.Errorf("Error scanning resultset %d: %v", i, err)
		}
		srss = append(srss, *srs)
	}
	return srss, nil
}

type scanConn struct {
	dbConn
}

// Query implements Conn.
func (s *scanConn) Query(q string) ([]ScannedResultSet, error) {
	rss, err := s.dbConn.query(q)
	if err != nil {
		return nil, err
	}
	return scanResultSets(rss)
}

// Close implements Conn.
func (s scanConn) Close() error {
	return s.dbConn.Close()
}

func Open(driverName, dsn string) (Conn, error) {
	driversMu.RLock()
	driveri, ok := drivers[driverName]
	driversMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("sql: unknown driver %q (forgotten import?)", driverName)
	}
	log.Debugf("opening %s connection", driverName)
	conn, err := driveri.Open(dsn)
	if err != nil {
		return nil, err
	}
	return &scanConn{dbConn: conn}, nil
}

func dbStringToFloat64(s string, re *regexp.Regexp) (float64, bool) {
	if re != nil {
		ss := re.FindStringSubmatch(s)
		if len(ss) > 1 {
			s = ss[1]
		}
	}
	result, err := strconv.ParseFloat(s, 64)
	if err != nil {
		log.Infoln("Could not parse string:", err)
		return math.NaN(), false
	}

	return result, true
}

// Convert database.sql types to float64s for Prometheus consumption. Null
// types are mapped to NaN. string and []byte types are mapped as NaN and !ok
func ToFloat64(t interface{}, r *regexp.Regexp) (float64, bool) {
	switch v := t.(type) {
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case float32:
		return float64(v), true
	case float64:
		return v, true
	case time.Time:
		return float64(v.Unix()), true
	case []byte:
		// Try and convert to string and then parse to a float64
		return dbStringToFloat64(string(v), r)
	case string:
		return dbStringToFloat64(v, r)
	case nil:
		return math.NaN(), true
	default:
		return math.NaN(), false
	}
}

func ToUnsignedFloat64(t interface{}, r *regexp.Regexp) (float64, bool) {
	switch v := t.(type) {
	case int32:
		return float64(uint32(v)), true
	case int64:
		return float64(uint64(v)), true
	default:
		return ToFloat64(t, r)
	}
}

// Convert database.sql to string for Prometheus labels. Null types are mapped to empty strings.
func ToString(t interface{}) (string, bool) {
	switch v := t.(type) {
	case int64:
		return fmt.Sprintf("%v", v), true
	case float64:
		return fmt.Sprintf("%v", v), true
	case time.Time:
		return fmt.Sprintf("%v", v.Unix()), true
	case nil:
		return "", true
	case []byte:
		// Try and convert to string
		return string(v), true
	case string:
		return v, true
	default:
		return "", false
	}
}
