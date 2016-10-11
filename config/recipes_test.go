package config

import (
	"github.com/ncabatoff/dbms_exporter/db"
	"testing"
)

type mockConn struct {
	sqls  []string
	rsets []db.ScannedResultSet
}

func (c *mockConn) Query(q string) ([]db.ScannedResultSet, error) {
	c.sqls = append(c.sqls, q)
	return c.rsets, nil
}

func (c *mockConn) Close() error {
	return nil
}

func TestGetRecipesSimple(t *testing.T) {
	recipe := `
  recipe1:
    metrics:
      - met1:
          usage: DISCARD
          description: desc1
      - met2:
          usage: LABEL
          description: desc2
      - met3:
          usage: COUNTER
          description: desc3
          regexp: ^(abc)$
      - met4:
          usage: GAUGE
          description: desc4
      - met5:
          usage: MAPPEDMETRIC
          description: desc5
      - met6:
          usage: DURATION
          description: desc6
      - met7:
          usage: FIXED
          description: desc7
          value: somevalue`
	var rows [][]interface{}
	testGetRecipesSingleResultSet(t, recipe, "recipe1",
		[]string{"met1", "met2", "met3", "met4", "met5", "met6", "met7"},
		rows,
		[]string{"select * from recipe1"})
}

func TestGetRecipesSimpleQuery(t *testing.T) {
	recipe := `
  recipe2:
    query: select * from a
    metrics:
      - met1:
          usage: DISCARD
          description: desc1
    `
	var rows [][]interface{}
	testGetRecipesSingleResultSet(t, recipe, "recipe2",
		[]string{"met1"},
		rows,
		[]string{"select * from a"})
}

/* TODO enable once there's a testGetRecipesMultipleResultSets
func TestGetRecipesSimpleQueries(t *testing.T) {
	recipe := `
  recipe2:
    queries:
      - use db
      - select * from a
    metrics:
      - met1:
          usage: DISCARD
          description: desc1
    `
	var rows [][]interface{}
	testGetRecipesSingleResultSet(t, recipe, "recipe2",
		[]string{"met1"},
		rows,
		[]string{"use db", "select * from a"})
}
*/

func testGetRecipesSingleResultSet(t *testing.T, recipe, namespace string, colnames []string, rows [][]interface{}, sqls []string) {
	rs, err := GetRecipes("test", recipe)

	if err != nil {
		t.Fatalf("unable to parse recipe: %v", err)
	}

	if len(rs) != 1 {
		t.Fatalf("did not find 1 recipe")
	}
	r := rs[0]

	// Test that the namespace is correct (prefix + recipe name).
	wantNamespace := "test_" + namespace
	if r.GetNamespace() != wantNamespace {
		t.Errorf("recipe basename is %q, want %q", r.GetNamespace(), wantNamespace)
	}

	// Test that the correct number of resultmaps and columns are present.
	mmaps := r.GetResultMaps()
	if len(mmaps) != 1 {
		t.Fatalf("got %d resultmaps, want %d", len(mmaps), 1)
	}
	if len(mmaps[0].ResultMap) != len(colnames) {
		t.Fatalf("got %d metrics, want %d", len(mmaps[0].ResultMap), len(colnames))
	}

	// Test that the SQL(s) are correct
	mc := &mockConn{rsets: []db.ScannedResultSet{db.ScannedResultSet{
		Colnames: colnames,
		Rows:     rows,
	},
	}}

	srss, err := r.Run(mc)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if len(srss) != 1 {
		t.Fatalf("Run yielded %d resultsets, want %d", len(srss), 1)
	}

	if len(mc.sqls) != len(sqls) {
		t.Fatalf("recipe queries are %#v, want %#v", mc.sqls, sqls)
	}
	for i, s := range mc.sqls {
		if sqls[i] != s {
			t.Errorf("recipe query %d is %q, want %q", i, s, sqls[i])
		}
	}
}
