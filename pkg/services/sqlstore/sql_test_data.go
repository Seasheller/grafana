package sqlstore

import (
	"math/rand"
	"time"

	"github.com/Seasheller/grafana/pkg/bus"
	m "github.com/Seasheller/grafana/pkg/models"
)

func init() {
	bus.AddHandler("sql", InsertSqlTestData)
}

func sqlRandomWalk(m1 string, m2 string, intWalker int64, floatWalker float64, sess *DBSession) error {

	timeWalker := time.Now().UTC().Add(time.Hour * -200)
	now := time.Now().UTC()
	step := time.Minute

	row := &m.SqlTestData{
		Metric1:      m1,
		Metric2:      m2,
		TimeEpoch:    timeWalker.Unix(),
		TimeDateTime: timeWalker,
	}

	for timeWalker.Unix() < now.Unix() {
		timeWalker = timeWalker.Add(step)

		row.Id = 0
		row.ValueBigInt += rand.Int63n(200) - 100
		row.ValueDouble += rand.Float64() - 0.5
		row.ValueFloat += rand.Float32() - 0.5
		row.TimeEpoch = timeWalker.Unix()
		row.TimeDateTime = timeWalker

		sqlog.Info("Writing SQL test data row")
		if _, err := sess.Table("test_data").Insert(row); err != nil {
			return err
		}
	}

	return nil
}

func InsertSqlTestData(cmd *m.InsertSqlTestDataCommand) error {
	return inTransaction(func(sess *DBSession) error {
		var err error

		sqlog.Info("SQL TestData: Clearing previous test data")
		res, err := sess.Exec("TRUNCATE test_data")
		if err != nil {
			return err
		}

		rows, _ := res.RowsAffected()
		sqlog.Info("SQL TestData: Truncate done", "rows", rows)

		sqlRandomWalk("server1", "frontend", 100, 1.123, sess)
		sqlRandomWalk("server2", "frontend", 100, 1.123, sess)
		sqlRandomWalk("server3", "frontend", 100, 1.123, sess)

		return err
	})
}
