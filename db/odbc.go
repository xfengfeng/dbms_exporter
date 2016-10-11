// +build odbc

package db

import _ "github.com/alexbrainman/odbc"

func init() {
	name := "odbc"
	drv := dsqlDrv(name)
	Register(name, &drv)
}
