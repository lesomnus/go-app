-- Create "tenant" table
CREATE TABLE "tenant" ("id" uuid NOT NULL, "alias" character varying NOT NULL, "name" character varying NOT NULL, "desc" character varying NOT NULL, "labels" jsonb NULL, "date_created" timestamptz NULL, PRIMARY KEY ("id"));
-- Create index "tenant_alias_key" to table: "tenant"
CREATE UNIQUE INDEX "tenant_alias_key" ON "tenant" ("alias");
-- Create "user" table
CREATE TABLE "user" ("id" uuid NOT NULL, "alias" character varying NOT NULL, "name" character varying NOT NULL, "desc" character varying NOT NULL, "labels" jsonb NULL, "date_created" timestamptz NULL, "user_tenant" uuid NOT NULL, PRIMARY KEY ("id"), CONSTRAINT "user_tenant_tenant" FOREIGN KEY ("user_tenant") REFERENCES "tenant" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION);
-- Create index "user_alias_user_tenant" to table: "user"
CREATE UNIQUE INDEX "user_alias_user_tenant" ON "user" ("alias", "user_tenant");
