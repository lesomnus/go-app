-- Run once, when the database container is created for the first time.

-- The dev database `migrate plan` works the migrations out on. It is emptied
-- every time it is used, so nothing may live in it.
CREATE DATABASE dev;
