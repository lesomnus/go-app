-- Create "roaster" table
CREATE TABLE "roaster" ("id" uuid NOT NULL, "alias" character varying NOT NULL, "name" character varying NOT NULL, "desc" character varying NOT NULL, "labels" jsonb NULL, "date_created" timestamptz NULL, PRIMARY KEY ("id"));
-- Create index "roaster_alias_key" to table: "roaster"
CREATE UNIQUE INDEX "roaster_alias_key" ON "roaster" ("alias");
-- Create "coffee" table
CREATE TABLE "coffee" ("id" uuid NOT NULL, "alias" character varying NOT NULL, "name" character varying NOT NULL, "desc" character varying NOT NULL, "labels" jsonb NULL, "date_erased" timestamptz NULL, "date_created" timestamptz NULL, "coffee_roaster" uuid NOT NULL, PRIMARY KEY ("id"), CONSTRAINT "coffee_roaster_roaster" FOREIGN KEY ("coffee_roaster") REFERENCES "roaster" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION);
-- Create index "coffee_alias_coffee_roaster" to table: "coffee"
CREATE UNIQUE INDEX "coffee_alias_coffee_roaster" ON "coffee" ("alias", "coffee_roaster") WHERE (date_erased IS NULL);
