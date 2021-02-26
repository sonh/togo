package postgres

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/manabie-com/togo/internal/storages"
	"github.com/pkg/errors"
	"time"
)

type Config struct {
	Host string
	Port string
	Usr  string
	Pwd  string
	Db   string
}

func (c *Config) toConnStr() string {
	return fmt.Sprintf("postgresql://%s:%s@%s:%s/%s", c.Usr, c.Pwd, c.Host, c.Port, c.Db)
}

type Postgres struct {
	pool *pgxpool.Pool
}

func NewPostgres(ctx context.Context) (*Postgres, error) {
	var connStr string
	switch v := ctx.Value("config").(type) {
	case *Config:
		connStr = v.toConnStr()
	default:
		return nil, errors.New("no config")
	}

	pool, err := pgxpool.Connect(ctx, connStr)
	if err != nil {
		return nil, errors.Wrap(err, "Connect()")
	}

	pg := &Postgres{
		pool: pool,
	}

	if err := pg.init(ctx); err != nil {
		return nil, errors.Wrap(err, "init()")
	}

	return pg, nil
}

func (pg *Postgres) init(ctx context.Context) error {
	stmt :=
		`
		CREATE EXTENSION IF NOT EXISTS pgcrypto;

		CREATE TABLE IF NOT EXISTS usr (
		    id 			int GENERATED ALWAYS AS IDENTITY PRIMARY KEY ,
		    username	varchar(36) NOT NULL UNIQUE ,
		    pwd_hash 	text NOT NULL ,
		    max_todo 	int NOT NULL DEFAULT 5 CHECK ( max_todo >= 0 )
		);
		CREATE TABLE IF NOT EXISTS task (
		  	id 			int GENERATED ALWAYS AS IDENTITY PRIMARY KEY ,
		  	usr_id 		int NOT NULL REFERENCES usr(id),
		  	content 	text NOT NULL ,
		  	create_at	timestamptz NOT NULL
		);

		CREATE INDEX IF NOT EXISTS usr_username_pwd_hash_idx ON usr(username, pwd_hash);
		CREATE INDEX IF NOT EXISTS task_usr_id_idx ON task(usr_id);
		CREATE INDEX IF NOT EXISTS task_usr_id_create_at_idx ON task(usr_id);

		INSERT INTO usr (
			id,
			username, 
			pwd_hash, 
			max_todo
		) OVERRIDING SYSTEM VALUE VALUES (
		    1,                              
			'firstUser',
		    crypt('example', gen_salt('bf')) ,
			5
		) ON CONFLICT DO NOTHING ;

		INSERT INTO task (
		                	id,
		                  usr_id, 
		                  content, 
		                  create_at) 
		                  OVERRIDING SYSTEM VALUE VALUES  (
		                        1,
							   1,
							   'test 1',
							   '2020-06-29'::timestamptz
		                  ) ON CONFLICT DO NOTHING ;
		`

	_, err := pg.pool.Exec(ctx, stmt)
	if err != nil {
		return errors.Wrap(err, "Exec()")
	}
	return nil
}

func (pg *Postgres) ValidateUser(ctx context.Context, username, password string) (*storages.PgUser, error) {
	stmt :=
		`
		SELECT 
			id,
			username,
			pwd_hash,
			max_todo
		FROM 
			usr
		WHERE 
			username = $1
			AND pwd_hash = crypt($2, pwd_hash)
		`
	row := pg.pool.QueryRow(ctx, stmt, username, password)

	usr := &storages.PgUser{}
	err := row.Scan(&usr.Id, &usr.Username, &usr.PwdHash, &usr.MaxTodo)

	switch err {
	case nil:
		return usr, nil
	case pgx.ErrNoRows:
		return nil, errors.New("username or password is not correct")
	default:
		return nil, errors.Wrap(err, "Scan()")
	}
}

func (pg *Postgres) GetTasks(ctx context.Context, usrId int, createAt time.Time) ([]*storages.PgTask, error) {
	stmt :=
		`
		SELECT 
			id, usr_id, content, create_at
		FROM 
		     task
		WHERE 
		      usr_id = $1
		      AND create_at::date = $2::date
		`

	rows, err := pg.pool.Query(ctx, stmt, usrId, createAt)
	switch err {
	case nil:
		defer rows.Close()
	case pgx.ErrNoRows:
		return nil, nil
	default:
		return nil, err
	}

	tasks := make([]*storages.PgTask, 0)
	for rows.Next() {
		task := &storages.PgTask{}
		err := rows.Scan(
			&task.Id,
			&task.UsrId,
			&task.Content,
			&task.CreateAt,
		)
		if err != nil {
			return nil, errors.Wrap(err, "Scan()")
		}
		tasks = append(tasks, task)
	}

	return tasks, nil
}

func (pg *Postgres) InsertTask(ctx context.Context, task *storages.PgTask) error {
	stmt :=
		`
		INSERT INTO 
		    task (usr_id, content, create_at)
		VALUES 
			($1, $2, now())
		;
		`

	cmd, err := pg.pool.Exec(ctx, stmt, task.UsrId, task.Content)
	if err != nil {
		return errors.Wrap(err, "Exec()")
	}

	if cmd.RowsAffected() < 1 {
		return errors.New("failed to insert, no rows affected")
	}

	return nil
}

func (pg *Postgres) Close() {
	pg.pool.Close()
}