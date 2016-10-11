package db

import (
	"database/sql"
)

type dsqlDrv string

func (d *dsqlDrv) Open(dsn string) (dbConn, error) {
	return openDatabaseSqlConn(string(*d), dsn)
}

func openDatabaseSqlConn(driver, dsn string) (dbConn, error) {
	conn, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, err
	}
	return &sqlDatabase{conn}, nil
}

type sqlDatabase struct {
	*sql.DB
}

// query implements dbConn.
func (sdb *sqlDatabase) query(sql string) ([]dbResultSet, error) {
	rs, err := sdb.DB.Query(sql)
	if err != nil {
		return nil, err
	}
	return []dbResultSet{rs}, nil
}
