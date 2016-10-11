// +build freetds

package db

import (
	"github.com/minus5/gofreetds"
)

func init() {
	Register("freetds", &freeTdsDrv{})
}

type freeTdsDrv struct{}

func (d *freeTdsDrv) Open(dsn string) (dbConn, error) {
	return openTdsDb(dsn)
}

type freetdsDatabase struct {
	conn *freetds.Conn
}

func openTdsDb(dsn string) (*freetdsDatabase, error) {
	conn, err := freetds.NewConn(dsn)
	if err != nil {
		return nil, err
	}
	return &freetdsDatabase{conn}, nil
}

func (fdb *freetdsDatabase) Close() error {
	fdb.conn.Close()
	return nil
}

func (fdb *freetdsDatabase) query(sql string) ([]dbResultSet, error) {
	r, err := fdb.conn.Exec(sql)
	if err != nil {
		return nil, err
	}
	for len(r) > 0 && len(r[0].Columns) == 0 {
		r = r[1:]
	}
	rss := make([]dbResultSet, len(r))
	for i := range r {
		rss[i] = &fdbResultSet{r[i]}
	}
	return rss, nil

}

type fdbResultSet struct {
	*freetds.Result
}

func (frs *fdbResultSet) Close() error {
	return nil
}

func (frs *fdbResultSet) Columns() ([]string, error) {
	cols := make([]string, len(frs.Result.Columns))
	for i, c := range frs.Result.Columns {
		cols[i] = c.Name
	}
	return cols, nil
}
