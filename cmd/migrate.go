package cmd

import (
	"context"
	"errors"
	"fmt"

	"github.com/lesomnus/xli"
	"github.com/lesomnus/xli/arg"
	"github.com/lesomnus/xli/flg"
	"github.com/lesomnus/z"

	"github.com/lesomnus/go-app/cmd/config"
	"github.com/lesomnus/go-app/internal/migrate"
)

func NewCmdMigrate() *xli.Command {
	return &xli.Command{
		Name:  "migrate",
		Brief: "plan and apply the versioned migrations",

		Commands: []*xli.Command{
			NewCmdMigratePlan(),
			NewCmdMigrateApply(),
		},

		Handler: xli.RequireSubcommand(),
	}
}

func NewCmdMigratePlan() *xli.Command {
	return &xli.Command{
		Name:  "plan",
		Brief: "write the migration that brings the database to the schema",

		Args: arg.Args{
			&arg.String{Name: "name", Brief: "what the migration is about, e.g. \"add_user_email\""},
		},
		Flags: flg.Flags{
			&flg.String{Name: "dir", Brief: "directory the migration files are kept in"},
			&flg.String{Name: "dev", Brief: "dsn of the dev database to plan with"},
		},

		Handler: xli.OnRun(func(ctx context.Context, cmd *xli.Command, next xli.Next) error {
			c := use_config.Must(ctx)

			path := migrate.DefaultDir
			flg.VisitP(cmd, "dir", &path)
			flg.VisitP(cmd, "dev", &c.Db.DevDsn)
			if c.Db.DevDsn == "" {
				return errors.New("no dev database given: it must be `--dev` or `db.dev_dsn` of the config")
			}

			dir, err := migrate.OpenDir(path)
			if err != nil {
				return z.Err(err, "open migration directory")
			}

			// The dev database is the kind the migrations are written for,
			// whatever the app itself is configured to run on; planning is
			// about the files, not about this deployment.
			driver, ok := config.DriverFor(migrate.Dialect)
			if !ok {
				return fmt.Errorf("no driver of the registered ones speaks %s: %v", migrate.Dialect, config.Drivers())
			}

			db, err := config.DbConfig{Driver: driver, Dsn: c.Db.DevDsn}.Open(ctx)
			if err != nil {
				return z.Err(err, "open dev database")
			}
			defer db.Close()

			name := arg.MustGet[string](cmd, "name")
			fs, err := migrate.Plan(ctx, dir, db.Conn(), db.Dialect(), name)
			if err != nil {
				return z.Err(err, "plan")
			}
			if len(fs) == 0 {
				cmd.Println("the database is already what the schema says; nothing was written")
				return nil
			}

			for _, f := range fs {
				cmd.Println("written:", f.Name())
			}
			cmd.Println("read them before they are applied to anything.")

			return nil
		}),
	}
}

func NewCmdMigrateApply() *xli.Command {
	return &xli.Command{
		Name:  "apply",
		Brief: "run the migrations the database did not run yet",

		Flags: flg.Flags{
			&flg.String{Name: "dir", Brief: "directory the migration files are kept in"},
			&flg.Switch{Name: "dry-run", Brief: "print what is pending and stop"},
		},

		Handler: xli.OnRun(func(ctx context.Context, cmd *xli.Command, next xli.Next) error {
			c := use_config.Must(ctx)

			path := migrate.DefaultDir
			flg.VisitP(cmd, "dir", &path)

			var dry bool
			flg.VisitP(cmd, "dry-run", &dry)

			dir, err := migrate.OpenDir(path)
			if err != nil {
				return z.Err(err, "open migration directory")
			}

			db, err := c.Db.Open(ctx)
			if err != nil {
				return z.Err(err, "open database")
			}
			defer db.Close()

			if err := speaks(db); err != nil {
				return err
			}

			if dry {
				fs, err := migrate.Pending(ctx, db.Conn(), db.Dialect(), dir)
				if err != nil {
					return z.Err(err, "read what is pending")
				}

				report(cmd, "pending", fs)
				return nil
			}

			fs, err := migrate.Apply(ctx, db.Conn(), db.Dialect(), dir)
			if err != nil {
				return z.Err(err, "apply")
			}

			report(cmd, "applied", fs)
			return nil
		}),
	}
}

// speaks refuses a database that does not speak the dialect the migration
// files are written in, since the statements would be the wrong ones.
func speaks(db *config.Db) error {
	if v := db.Dialect(); v != migrate.Dialect {
		return fmt.Errorf("the migrations are %s but the database is %s: see the migration guide in README.md", migrate.Dialect, v)
	}

	return nil
}

func report[T interface{ Name() string }](cmd *xli.Command, what string, fs []T) {
	if len(fs) == 0 {
		cmd.Println("the database is up to date")
		return
	}

	for _, f := range fs {
		cmd.Println(what+":", f.Name())
	}
}
