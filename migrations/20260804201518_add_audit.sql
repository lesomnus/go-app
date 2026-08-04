-- Create "audit" table
CREATE TABLE "audit" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "actor_id" uuid NOT NULL, "trace_id" bytea NOT NULL, "action" character varying NOT NULL, "object_id" uuid NOT NULL, "patch" bytea NOT NULL, "date_created" timestamptz NULL, PRIMARY KEY ("id"));
-- Create index "audit_object_id" to table: "audit"
CREATE INDEX "audit_object_id" ON "audit" ("object_id");
-- Create index "audit_tenant_id_date_created" to table: "audit"
CREATE INDEX "audit_tenant_id_date_created" ON "audit" ("tenant_id", "date_created");
