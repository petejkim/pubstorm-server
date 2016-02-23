Rise Server
===========

## Dependencies

### PostgreSQL

```shell
# Install PostgreSQL 9.4
sudo bash -c 'echo "deb http://apt.postgresql.org/pub/repos/apt/ trusty-pgdg main" > /etc/apt/sources.list.d/pgdg.list'
wget --quiet -O - https://www.postgresql.org/media/keys/ACCC4CF8.asc | sudo apt-key add -
sudo apt-get update
sudo apt-get install postgresql-9.4 libpq-dev postgresql-contrib-9.4
sudo su postgres -c 'createuser --superuser nitrous && createdb rise_development'
```

### Redis

```shell
# Install Redis 3.0
sudo add-apt-repository ppa:chris-lea/redis-server
sudo apt-get update
sudo apt-get install redis-server
```

## DB Migrations

```shell
# Install migrate
go get -u github.com/mattes/migrate
# Run migrations
script/migrate
# Creating a new migration
script/migrate-new 'create_animals'
```

## Test

```shell
# Install Ginkgo/Gomega
go get -u github.com/onsi/ginkgo/ginkgo
go get -u github.com/onsi/gomega
# Prepare test DB
script/migrate # Make sure dev DB is up to date
script/prepare-test-db # Copy dev DB's schema to test DB
# Run tests
script/test
```

- - -
Copyright (c) 2016 Nitrous, Inc. All Rights Reserved.
