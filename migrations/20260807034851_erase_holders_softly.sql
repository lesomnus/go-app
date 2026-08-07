-- Drop index "holder_alias_holder_tenant" from table: "holder"
DROP INDEX "holder_alias_holder_tenant";
-- Modify "holder" table
ALTER TABLE "holder" ADD COLUMN "date_erased" timestamptz NULL;
-- Create index "holder_alias_holder_tenant" to table: "holder"
CREATE UNIQUE INDEX "holder_alias_holder_tenant" ON "holder" ("alias", "holder_tenant") WHERE (date_erased IS NULL);
