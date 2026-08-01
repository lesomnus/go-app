# migrations

The versioned migrations of the database, in the order they are applied.

Nothing here is written by hand. `migrate plan` works out what changed between
the ent schema and what the files already describe, and writes the difference
as a new file; `atlas.sum` records what each file looked like, so a file that
is edited after the fact is refused.

```sh
$ docker compose up -d db          # the dev database the plan is worked out on
$ go run . migrate plan add_user_email
$ go run . migrate apply --dry-run
```

The statements are PostgreSQL. See the migration guide in the README at the
root of the repository for the whole story, including what it takes to run on
another database.
