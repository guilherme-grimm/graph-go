CREATE TABLE customers (id serial PRIMARY KEY, name text NOT NULL);
CREATE TABLE orders (id serial PRIMARY KEY, customer_id int NOT NULL REFERENCES customers (id));
