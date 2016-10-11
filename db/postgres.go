// +build postgres

package db

import _ "github.com/lib/pq"

func init() {
	name := "postgres"
	drv := dsqlDrv(name)
	Register(name, &drv)
}
