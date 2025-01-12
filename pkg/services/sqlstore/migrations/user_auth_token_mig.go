package migrations

import (
	. "github.com/Seasheller/grafana/pkg/services/sqlstore/migrator"
)

func addUserAuthTokenMigrations(mg *Migrator) {
	userAuthTokenV1 := Table{
		Name: "user_auth_token",
		Columns: []*Column{
			{Name: "id", Type: DB_BigInt, IsPrimaryKey: true, IsAutoIncrement: true},
			{Name: "user_id", Type: DB_BigInt, Nullable: false},
			{Name: "auth_token", Type: DB_NVarchar, Length: 100, Nullable: false},
			{Name: "prev_auth_token", Type: DB_NVarchar, Length: 100, Nullable: false},
			{Name: "user_agent", Type: DB_NVarchar, Length: 255, Nullable: false},
			{Name: "client_ip", Type: DB_NVarchar, Length: 255, Nullable: false},
			{Name: "auth_token_seen", Type: DB_Bool, Nullable: false},
			{Name: "seen_at", Type: DB_Int, Nullable: true},
			{Name: "rotated_at", Type: DB_Int, Nullable: false},
			{Name: "created_at", Type: DB_Int, Nullable: false},
			{Name: "updated_at", Type: DB_Int, Nullable: false},
		},
		Indices: []*Index{
			{Cols: []string{"auth_token"}, Type: UniqueIndex},
			{Cols: []string{"prev_auth_token"}, Type: UniqueIndex},
		},
	}

	mg.AddMigration("create user auth token table", NewAddTableMigration(userAuthTokenV1))
	mg.AddMigration("add unique index user_auth_token.auth_token", NewAddIndexMigration(userAuthTokenV1, userAuthTokenV1.Indices[0]))
	mg.AddMigration("add unique index user_auth_token.prev_auth_token", NewAddIndexMigration(userAuthTokenV1, userAuthTokenV1.Indices[1]))
}
