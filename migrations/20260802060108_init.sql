-- Create "tenant" table
CREATE TABLE "tenant" ("id" uuid NOT NULL, "alias" character varying NOT NULL, "name" character varying NOT NULL, "desc" character varying NOT NULL, "labels" jsonb NULL, "date_created" timestamptz NULL, PRIMARY KEY ("id"));
-- Create index "tenant_alias_key" to table: "tenant"
CREATE UNIQUE INDEX "tenant_alias_key" ON "tenant" ("alias");
-- Create "holder" table
CREATE TABLE "holder" ("id" uuid NOT NULL, "alias" character varying NOT NULL, "name" character varying NOT NULL, "desc" character varying NOT NULL, "labels" jsonb NULL, "date_created" timestamptz NULL, "holder_tenant" uuid NOT NULL, PRIMARY KEY ("id"), CONSTRAINT "holder_tenant_tenant" FOREIGN KEY ("holder_tenant") REFERENCES "tenant" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION);
-- Create index "holder_alias_holder_tenant" to table: "holder"
CREATE UNIQUE INDEX "holder_alias_holder_tenant" ON "holder" ("alias", "holder_tenant");
